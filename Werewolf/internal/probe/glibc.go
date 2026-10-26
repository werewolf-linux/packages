package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

// GlibcOptions names the built loader and the C fixtures that exercise it.
type GlibcOptions struct {
	Build          string   `yaml:"build"`
	Fixtures       string   `yaml:"fixtures"`
	CC             []string `yaml:"cc,omitempty"`
	CXX            []string `yaml:"cxx,omitempty"`
	Runner         []string `yaml:"runner,omitempty"`
	BaselineLoader string   `yaml:"baseline-loader,omitempty"`
	Report         string   `yaml:"report,omitempty"`
	Secure         bool     `yaml:"secure,omitempty"`
	TraceSpawn     bool     `yaml:"trace-spawn,omitempty"`
}

type result struct {
	stdout, stderr string
	code           int
}

func execute(ctx context.Context, timeout time.Duration, directory string, environment []string, args ...string) (result, error) {
	if len(args) == 0 {
		return result{}, fmt.Errorf("empty probe command")
	}
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = directory
	command.Env = environment
	command.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return result{}, err
	}
	var timer *time.Timer
	expired := make(chan struct{})
	if timeout > 0 {
		timer = time.AfterFunc(timeout, func() {
			command.Process.Kill()
			close(expired)
		})
	}
	err := command.Wait()
	if timer != nil && !timer.Stop() {
		<-expired
		return result{}, fmt.Errorf("probe command exceeded %s: %v", timeout, args)
	}
	if ctx.Err() != nil {
		return result{}, ctx.Err()
	}
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			return result{}, err
		}
		code = exit.ExitCode()
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}, nil
}

func checked(ctx context.Context, args ...string) (result, error) {
	value, err := execute(ctx, 0, "", nil, args...)
	if err != nil {
		return value, err
	}
	if value.code != 0 {
		return value, fmt.Errorf("%v exited %d: %s%s", args, value.code, value.stdout, value.stderr)
	}
	return value, nil
}

func environment(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	slices.Sort(result)
	return result
}

// Glibc validates the patched loader and libc using the actual compiled ELF
// fixtures, including positive controls that prove attempted injection works.
func Glibc(ctx context.Context, options GlibcOptions) error {
	build, err := filepath.Abs(options.Build)
	if err != nil {
		return err
	}
	fixtures, err := filepath.Abs(options.Fixtures)
	if err != nil {
		return err
	}
	loader := filepath.Join(build, "elf/ld.so")
	for _, path := range []string{loader, filepath.Join(build, "libc.so")} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("missing built libc object: %s", path)
		}
	}
	cc, cxx := options.CC, options.CXX
	if len(cc) == 0 {
		cc = []string{"gcc"}
	}
	if len(cxx) == 0 {
		cxx = []string{"g++"}
	}
	compiler, err := checked(ctx, append(slices.Clone(cc), "--version")...)
	if err != nil {
		return err
	}
	resolved, err := checked(ctx, append(slices.Clone(cc), "-###", "-O2", "-c", "-x", "c", "-")...)
	if err != nil {
		return err
	}
	evidence := map[string]any{"compiler": compiler.stdout, "resolved_compiler_options": resolved.stderr}
	if _, err := checked(ctx, "make", "-C", build, "test", "t=elf/tst-werewolf-audit", "test-wrapper="+strings.Join(options.Runner, " ")); err != nil {
		return err
	}
	audit, err := checked(ctx, append(slices.Clone(options.Runner), filepath.Join(build, "elf/tst-werewolf-audit"))...)
	if err != nil {
		return err
	}
	undefined, err := checked(ctx, "nm", "-u", filepath.Join(build, "elf/dl-audit.os"))
	if err != nil {
		return err
	}
	if strings.Contains(undefined.stdout, "_rtld_global") {
		return fmt.Errorf("audit dispatch still reads loader state")
	}
	evidence["audit_dispatch"] = map[string]any{"status": "PASS", "output": audit.stdout, "undefined_symbols": undefined.stdout}
	objects := map[string]any{}
	for _, path := range []string{loader, filepath.Join(build, "libc.so")} {
		headers, err := checked(ctx, "readelf", "-lW", path)
		if err != nil {
			return err
		}
		dynamic, err := checked(ctx, "readelf", "-dW", path)
		if err != nil {
			return err
		}
		stack := 0
		firstLoad := []string{}
		for _, line := range strings.Split(headers.stdout, "\n") {
			if strings.Contains(line, "GNU_STACK") {
				stack++
				if !strings.Contains(line, "RW ") || strings.Contains(line, "RWE") {
					return fmt.Errorf("executable or unrecognized stack: %s", path)
				}
			}
			fields := strings.Fields(line)
			if len(firstLoad) == 0 && len(fields) > 0 && fields[0] == "LOAD" {
				firstLoad = fields
			}
		}
		if stack != 1 || !strings.Contains(headers.stdout, "GNU_RELRO") || (!strings.Contains(dynamic.stdout, "BIND_NOW") && !strings.Contains(dynamic.stdout, "Flags: NOW")) || len(firstLoad) < 8 || strings.Contains(strings.Join(firstLoad[6:len(firstLoad)-1], ""), "E") {
			return fmt.Errorf("ELF hardening evidence failed: %s", path)
		}
		notes, err := checked(ctx, "readelf", "-nW", path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(build, path)
		if err != nil {
			return err
		}
		objects[relative] = map[string]string{"program_headers": headers.stdout, "dynamic_tags": dynamic.stdout, "notes": notes.stdout}
	}
	evidence["elf"] = objects
	work, err := os.MkdirTemp("", "neutered-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	marker := filepath.Join(work, "executed")
	base := map[string]string{}
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		if !strings.HasPrefix(key, "LD_") && key != "GLIBC_TUNABLES" {
			base[key] = value
		}
	}
	base["NEUTERED_PUBLIC"] = "application-value"
	base["NEUTERED_MARKER"] = marker
	base["NEUTERED_CONVERTERS"] = work
	libraries := []string{build, filepath.Join(build, "math"), filepath.Join(build, "elf"), work}
	for _, compiler := range []struct {
		command []string
		library string
	}{{cc, "libgcc_s.so.1"}, {cxx, "libstdc++.so"}} {
		value, err := checked(ctx, append(slices.Clone(compiler.command), "-print-file-name="+compiler.library)...)
		if err != nil {
			return err
		}
		path := strings.TrimSpace(value.stdout)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			libraries = append(libraries, filepath.Dir(path))
		}
	}
	flags := []string{"-O2", "-Wall", "-Wextra", "-Werror", "-L" + build, "-Wl,--disable-new-dtags", "-Wl,-rpath," + strings.Join(libraries, ":"), "-Wl,-rpath-link," + filepath.Join(build, "elf"), "-Wl,--dynamic-linker=" + loader}
	compile := func(name, source string, extra ...string) error {
		path := source
		if !filepath.IsAbs(path) {
			path = filepath.Join(fixtures, path)
		}
		args := slices.Clone(flags)
		support := []string{filepath.Join(build, "libc_nonshared.a"), loader}
		if slices.Contains(extra, "-static") {
			args = slices.DeleteFunc(args, func(value string) bool { return strings.HasPrefix(value, "-Wl,--dynamic-linker=") })
			args = append(args, "-B"+filepath.Join(build, "csu")+"/", "-no-pie")
			support = nil
		}
		compiler := cc
		if strings.HasSuffix(source, ".cc") {
			compiler = cxx
		}
		command := append(slices.Clone(compiler), args...)
		command = append(command, extra...)
		command = append(command, path)
		command = append(command, support...)
		command = append(command, "-o", filepath.Join(work, name))
		_, err := checked(ctx, command...)
		return err
	}
	run := func(args []string, changes map[string]string, success bool, directory string) (result, error) {
		env := map[string]string{}
		for key, value := range base {
			env[key] = value
		}
		for key, value := range changes {
			env[key] = value
		}
		prefix := slices.Clone(options.Runner)
		if len(prefix) > 0 {
			for key, value := range env {
				if strings.HasPrefix(key, "LD_") || key == "GLIBC_TUNABLES" {
					prefix = append(prefix, "-E", key+"="+value)
					delete(env, key)
				}
			}
		}
		value, err := execute(ctx, 30*time.Second, directory, environment(env), append(prefix, args...)...)
		if err != nil {
			return value, err
		}
		if (value.code == 0) != success {
			return value, fmt.Errorf("%v exited %d: %s%s", args, value.code, value.stdout, value.stderr)
		}
		return value, nil
	}
	absent := func() error {
		if _, err := os.Lstat(marker); !os.IsNotExist(err) {
			return fmt.Errorf("injection marker unexpectedly exists: %s", marker)
		}
		return nil
	}
	binary := filepath.Join(work, "policy")
	dso := filepath.Join(work, "inject.so")
	if err := compile("policy", "policy.c", "-pthread", "-ldl"); err != nil {
		return err
	}
	if err := compile("inject.so", "inject.c", "-shared", "-fPIC"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "gconv-modules"), []byte("module INTERNAL NEUTERED-PROBE// inject 1\n"), 0644); err != nil {
		return err
	}
	if _, err := run([]string{binary, "load", dso}, nil, true, ""); err != nil {
		return err
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "injected\n" {
		return fmt.Errorf("positive injection control did not execute")
	}
	if err := os.Remove(marker); err != nil {
		return err
	}
	for _, test := range []struct {
		args []string
		out  string
	}{{[]string{binary}, ""}, {[]string{binary, "command-path"}, "implicit command lookup denied\n"}, {[]string{binary, "exec-explicit", binary}, "explicit exec passed\n"}} {
		value, err := run(test.args, nil, true, "")
		if err != nil {
			return err
		}
		if test.out != "" && value.stdout != test.out {
			return fmt.Errorf("unexpected explicit-command result: %s", value.stdout)
		}
	}
	if err := compile("unwind", "unwind.cc"); err != nil {
		return err
	}
	if _, err := run([]string{filepath.Join(work, "unwind")}, nil, true, ""); err != nil {
		return err
	}
	for _, test := range []struct {
		name  string
		extra []string
	}{{"ifunc", nil}, {"ifunc.so", []string{"-shared", "-fPIC", "-DIFUNC_DSO"}}, {"ifunc-load", []string{"-DIFUNC_LOAD", "-ldl"}}} {
		if err := compile(test.name, "ifunc.c", test.extra...); err != nil {
			return err
		}
	}
	symbols, err := checked(ctx, "readelf", "-sW", filepath.Join(work, "ifunc.so"))
	if err != nil {
		return err
	}
	found := false
	for _, line := range strings.Split(symbols.stdout, "\n") {
		found = found || strings.Contains(line, "IFUNC") && strings.Contains(line, "neutered_ifunc")
	}
	if !found {
		return fmt.Errorf("IFUNC fixture lost its resolver")
	}
	for _, args := range [][]string{{filepath.Join(work, "ifunc")}, {filepath.Join(work, "ifunc-load"), filepath.Join(work, "ifunc.so")}} {
		if _, err := run(args, nil, true, ""); err != nil {
			return err
		}
	}
	evidence["ifunc"] = map[string]any{"status": "PASS", "paths": []string{"startup", "dlopen", "dlmopen", "dlsym"}}
	unresolvedSource := filepath.Join(work, "unresolved.c")
	if err := os.WriteFile(unresolvedSource, []byte("extern int neutered_missing_symbol(void);\nint neutered_call(void) { return neutered_missing_symbol(); }\n"), 0644); err != nil {
		return err
	}
	if err := compile("unresolved.so", unresolvedSource, "-shared", "-fPIC", "-Wl,-z,lazy"); err != nil {
		return err
	}
	unresolved := filepath.Join(work, "unresolved.so")
	dynamic, err := checked(ctx, "readelf", "-dW", unresolved)
	if err != nil {
		return err
	}
	if strings.Contains(dynamic.stdout, "BIND_NOW") {
		return fmt.Errorf("lazy-binding fixture is not lazy")
	}
	value, err := run([]string{binary, "load-lazy", unresolved}, nil, false, "")
	if err != nil {
		return err
	}
	if !strings.Contains(value.stderr, "neutered_missing_symbol") {
		return fmt.Errorf("lazy unresolved symbol was not diagnosed")
	}
	for _, mode := range []struct {
		name  string
		flags []string
	}{{"dynamic", nil}, {"static", []string{"-static"}}} {
		name := "execstack-" + mode.name
		if err := compile(name, "policy.c", append(mode.flags, "-Wl,-z,execstack", "-pthread", "-ldl")...); err != nil {
			return err
		}
		executable := filepath.Join(work, name)
		headers, err := checked(ctx, "readelf", "-lW", executable)
		if err != nil {
			return err
		}
		executableStack := false
		for _, line := range strings.Split(headers.stdout, "\n") {
			executableStack = executableStack || strings.Contains(line, "GNU_STACK") && strings.Contains(line, "RWE")
		}
		if !executableStack {
			return fmt.Errorf("execstack fixture lost its executable stack")
		}
		value, err := run([]string{executable}, map[string]string{"GLIBC_TUNABLES": "glibc.rtld.execstack=2"}, false, "")
		if err != nil {
			return err
		}
		if !strings.Contains(value.stderr, "executable stack is not allowed") {
			return fmt.Errorf("executable stack was not refused")
		}
	}
	controls := map[string]string{
		"LD_PRELOAD": dso, "LD_AUDIT": dso, "LD_LIBRARY_PATH": work, "LD_DEBUG": "all", "LD_DEBUG_OUTPUT": filepath.Join(work, "debug"), "LD_PROFILE": "libc.so.6", "LD_PROFILE_OUTPUT": work,
		"GLIBC_TUNABLES": "glibc.malloc.tcache_count=0:glibc.malloc.arena_max=1", "MALLOC_ARENA_MAX": "1", "MALLOC_CHECK_": "3",
		"LANG": "neutered/nonexistent", "LANGUAGE": "neutered/nonexistent", "LC_ALL": "neutered/nonexistent", "LC_CTYPE": "neutered/nonexistent", "LOCPATH": work, "GCONV_PATH": work, "TZ": "GMT+11",
		"RES_OPTIONS": "rotate attempts:1", "HOSTALIASES": filepath.Join(work, "aliases"), "TMPDIR": filepath.Join(work, "missing"), "GMON_OUT_PREFIX": filepath.Join(work, "profile"),
	}
	value, err = run([]string{binary}, controls, true, "")
	if err != nil {
		return err
	}
	if value.stderr != "" {
		return fmt.Errorf("ignored settings produced diagnostic output: %s", value.stderr)
	}
	if err := absent(); err != nil {
		return err
	}
	for _, pattern := range []string{"debug*", "*.profile"} {
		paths, err := filepath.Glob(filepath.Join(work, pattern))
		if err != nil {
			return err
		}
		if len(paths) > 0 {
			return fmt.Errorf("ignored settings created files: %v", paths)
		}
	}
	defaults, err := run([]string{loader, "--list-tunables"}, nil, true, "")
	if err != nil {
		return err
	}
	if !strings.Contains(defaults.stdout, "glibc.rtld.execstack: 0 ") {
		return fmt.Errorf("execstack tunable default changed")
	}
	value, err = run([]string{loader, "--list-tunables"}, controls, true, "")
	if err != nil {
		return err
	}
	if value.stdout != defaults.stdout {
		return fmt.Errorf("environment changed loader tunables")
	}
	good, bad := filepath.Join(work, "good"), filepath.Join(work, "bad")
	if err := os.Mkdir(good, 0755); err != nil {
		return err
	}
	if err := os.Mkdir(bad, 0755); err != nil {
		return err
	}
	goodSource := filepath.Join(work, "good.c")
	if err := os.WriteFile(goodSource, []byte("int neutered_dependency(void) { return 42; }\n"), 0644); err != nil {
		return err
	}
	if err := compile("good/libneutered-needed.so.1", goodSource, "-shared", "-fPIC", "-Wl,-soname,libneutered-needed.so.1"); err != nil {
		return err
	}
	dsoData, err := os.ReadFile(dso)
	if err != nil {
		return err
	}
	badDSO := filepath.Join(bad, "libneutered-needed.so.1")
	if err := os.WriteFile(badDSO, dsoData, 0755); err != nil {
		return err
	}
	dependencySource := filepath.Join(work, "needed.c")
	if err := os.WriteFile(dependencySource, []byte("#include <assert.h>\nextern int neutered_dependency(void);\nint main(void) { assert(neutered_dependency() == 42); return 0; }\n"), 0644); err != nil {
		return err
	}
	dependency := filepath.Join(work, "needed")
	args := append(slices.Clone(cc), flags...)
	args = append(args, dependencySource, "-L"+good, "-l:libneutered-needed.so.1", loader, "-Wl,--enable-new-dtags", "-Wl,-rpath,"+good, "-o", dependency)
	if _, err := checked(ctx, args...); err != nil {
		return err
	}
	if _, err := run([]string{binary, "load", badDSO}, nil, true, ""); err != nil {
		return err
	}
	if _, err := os.Stat(marker); err != nil {
		return fmt.Errorf("dependency substitution control did not execute")
	}
	if err := os.Remove(marker); err != nil {
		return err
	}
	baseline := options.BaselineLoader
	if baseline == "auto" {
		baseline = map[string]string{"amd64": "/lib64/ld-linux-x86-64.so.2", "arm64": "/lib/ld-linux-aarch64.so.1"}[runtime.GOARCH]
		if baseline == "" {
			return fmt.Errorf("unsupported baseline loader architecture")
		}
	}
	if baseline != "" {
		if _, err := run([]string{baseline, "--library-path", strings.Join(append([]string{bad}, libraries...), ":"), dependency}, nil, true, ""); err != nil {
			return err
		}
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != "injected\n" {
			return fmt.Errorf("baseline substitution control did not execute")
		}
		if err := os.Remove(marker); err != nil {
			return err
		}
	}
	if _, err := run([]string{dependency}, map[string]string{"LD_LIBRARY_PATH": bad}, true, ""); err != nil {
		return err
	}
	if err := absent(); err != nil {
		return err
	}
	value, err = run([]string{loader, "--library-path", bad, dependency}, nil, false, "")
	if err != nil {
		return err
	}
	if !strings.Contains(value.stderr, "loader overrides are disabled") {
		return fmt.Errorf("loader library-path override was not refused")
	}
	if err := absent(); err != nil {
		return err
	}
	if _, err := run([]string{binary}, map[string]string{"GLIBC_TUNABLES": strings.Repeat("glibc.malloc.mxfast=", 2000), "LD_PRELOAD": dso + ":" + strings.Repeat("x", 32000)}, true, ""); err != nil {
		return err
	}
	for _, option := range []string{"--preload", "--audit", "--library-path", "--inhibit-rpath", "--inhibit-cache", "--glibc-hwcaps-prepend", "--glibc-hwcaps-mask"} {
		value, err := run([]string{loader, option, dso, binary}, nil, false, "")
		if err != nil {
			return err
		}
		if !strings.Contains(value.stderr, "loader overrides are disabled") {
			return fmt.Errorf("loader accepted %s", option)
		}
		if err := absent(); err != nil {
			return err
		}
	}
	for _, option := range []string{"--audit", "--depaudit"} {
		name := "elf-" + option[2:]
		if err := compile(name, "policy.c", "-pthread", "-ldl", "-Wl,"+option+","+dso); err != nil {
			return err
		}
		value, err := run([]string{filepath.Join(work, name)}, nil, false, "")
		if err != nil {
			return err
		}
		if !strings.Contains(value.stderr, "ELF audit requests are disabled") {
			return fmt.Errorf("ELF audit request was not refused")
		}
		if err := absent(); err != nil {
			return err
		}
	}
	if err := compile("static-policy", "policy.c", "-static", "-pthread", "-ldl"); err != nil {
		return err
	}
	static := filepath.Join(work, "static-policy")
	if _, err := run([]string{static}, controls, true, ""); err != nil {
		return err
	}
	value, err = run([]string{static, "command-path"}, nil, true, "")
	if err != nil {
		return err
	}
	if value.stdout != "implicit command lookup denied\n" {
		return fmt.Errorf("static implicit command lookup was not refused")
	}
	for _, mode := range []struct {
		name  string
		flags []string
	}{{"dynamic", nil}, {"static", []string{"-static"}}} {
		name := "profile-" + mode.name
		if err := compile(name, "benchmark.c", append(mode.flags, "-pg", "-pthread", "-Wl,--enable-new-dtags")...); err != nil {
			return err
		}
		executable := filepath.Join(work, name)
		prefix := filepath.Join(work, "profile-output")
		if baseline != "" && mode.name == "dynamic" {
			resolved, err := filepath.EvalSymlinks(baseline)
			if err != nil {
				return err
			}
			if _, err := run([]string{baseline, "--library-path", filepath.Dir(resolved), executable, "startup"}, map[string]string{"GMON_OUT_PREFIX": prefix}, true, work); err != nil {
				return err
			}
			outputs, err := filepath.Glob(prefix + ".*")
			if err != nil || len(outputs) == 0 {
				return fmt.Errorf("baseline profiling control did not produce output")
			}
			for _, path := range outputs {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}
		if _, err := run([]string{executable, "startup"}, map[string]string{"GMON_OUT_PREFIX": prefix}, true, work); err != nil {
			return err
		}
		outputs, err := filepath.Glob(prefix + ".*")
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(work, "gmon.out")); err == nil || len(outputs) > 0 {
			return fmt.Errorf("refused profiling path wrote output")
		}
	}
	if options.TraceSpawn {
		if len(options.Runner) > 0 {
			return fmt.Errorf("child environment tracing requires native execution")
		}
		for _, executable := range []string{binary, static} {
			for _, mode := range []string{"spawn-ifs", "wordexp-ifs"} {
				trace := filepath.Join(work, "spawn.trace")
				if _, err := run([]string{"strace", "-f", "-v", "-s", "65536", "-e", "trace=execve", "-o", trace, executable, mode}, nil, true, ""); err != nil {
					return err
				}
				data, err := os.ReadFile(trace)
				if err != nil {
					return err
				}
				var child []string
				for _, line := range strings.Split(string(data), "\n") {
					if strings.Contains(line, `execve("/bin/sh",`) {
						child = append(child, line)
					}
				}
				if len(child) != 1 || !strings.Contains(child[0], "NEUTERED_CHILD_ENV=forwarded") || strings.Contains(child[0], "IFS=NEUTERED_IFS_PROBE") != (mode == "spawn-ifs") {
					return fmt.Errorf("child environment sanitation differs for %s", mode)
				}
			}
		}
	}
	if err := os.WriteFile(filepath.Join(work, "libnss_neutered_probe.so.2"), dsoData, 0755); err != nil {
		return err
	}
	if _, err := run([]string{binary, "nss"}, nil, true, ""); err != nil {
		return err
	}
	if err := absent(); err != nil {
		return err
	}
	if options.Secure {
		if os.Geteuid() != 0 || len(options.Runner) > 0 {
			return fmt.Errorf("secure tests require native root")
		}
		if err := os.Chmod(work, 0755); err != nil {
			return err
		}
		for _, executable := range []string{binary, static} {
			if err := os.Chown(executable, 0, 0); err != nil {
				return err
			}
			if err := os.Chmod(executable, 0755|os.ModeSetuid); err != nil {
				return err
			}
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			for key, value := range controls {
				values[key] = value
			}
			command := exec.CommandContext(ctx, executable, "secure")
			command.Env = environment(values)
			command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("secure identity check: %w: %s", err, output)
			}
		}
	}
	if options.Report != "" {
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(options.Report, append(data, '\n'), 0644); err != nil {
			return err
		}
	}
	slog.Info("glibc policy probe passed", "build", build, "native", len(options.Runner) == 0)
	return nil
}
