/*
 * With executable-memory gain refused (PR_SET_MDWE), as werewolf refuses it
 * every process, a libffi closure must still be made and called, its code
 * must lie in libffi.so's own mapping, and no mapping may be both writable
 * and executable. Wolfi's libffi, without static trampolines, writes each
 * closure's code into a file it maps twice, writable and executable, which
 * MDWE allows but werewolf's noexec mounts and memfd_noexec refuse: its
 * closure code is then not the library's.
 */
#include <ffi.h>
#include <stdio.h>
#include <string.h>
#include <sys/prctl.h>

#ifndef PR_SET_MDWE
#define PR_SET_MDWE 65
#endif
#ifndef PR_MDWE_REFUSE_EXEC_GAIN
#define PR_MDWE_REFUSE_EXEC_GAIN 1
#endif

static void plus_one(ffi_cif *cif, void *ret, void **args, void *data)
{
	(void)cif;
	(void)data;
	*(ffi_arg *)ret = *(int *)args[0] + 1;
}

int main(void)
{
	ffi_cif cif;
	ffi_type *arg = &ffi_type_sint;
	ffi_closure *closure;
	void *code;
	char line[512], perms[5], path[256];
	unsigned long start, end;
	int in_library = 0;
	FILE *maps;

	if (prctl(PR_SET_MDWE, PR_MDWE_REFUSE_EXEC_GAIN, 0, 0, 0) != 0) {
		perror("PR_SET_MDWE");
		return 1;
	}
	closure = ffi_closure_alloc(sizeof(ffi_closure), &code);
	if (closure == NULL || ffi_prep_cif(&cif, FFI_DEFAULT_ABI, 1, &ffi_type_sint, &arg) != FFI_OK ||
	    ffi_prep_closure_loc(closure, &cif, plus_one, NULL, code) != FFI_OK) {
		fputs("libffi: no closure under MDWE\n", stderr);
		return 1;
	}
	if (((int (*)(int))code)(41) != 42) {
		fputs("libffi: the closure returned the wrong value\n", stderr);
		return 1;
	}
	maps = fopen("/proc/self/maps", "r");
	if (maps == NULL) {
		perror("/proc/self/maps");
		return 1;
	}
	while (fgets(line, sizeof(line), maps) != NULL) {
		path[0] = '\0';
		if (sscanf(line, "%lx-%lx %4s %*s %*s %*s %255s", &start, &end, perms, path) < 3)
			continue;
		if (strchr(perms, 'w') && strchr(perms, 'x')) {
			fprintf(stderr, "libffi: writable code: %s", line);
			return 1;
		}
		if ((unsigned long)code >= start && (unsigned long)code < end) {
			in_library = strstr(path, "/libffi.so") != NULL;
			if (!in_library)
				fprintf(stderr, "libffi: closure code outside the library: %s", line);
		}
	}
	if (!in_library)
		return 1;
	ffi_closure_free(closure);
	puts("libffi: closure called under MDWE, from the library's read-only code");
	return 0;
}
