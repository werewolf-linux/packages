#define _GNU_SOURCE
#include <assert.h>
#include <dlfcn.h>
#include <errno.h>
#include <getopt.h>
#include <fnmatch.h>
#include <iconv.h>
#include <locale.h>
#include <malloc.h>
#include <netdb.h>
#include <pwd.h>
#include <pthread.h>
#include <resolv.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/auxv.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
#include <wordexp.h>

extern int __nss_configure_lookup(const char *, const char *);
extern char **environ;

static void *thread(void *value)
{
    char *p = malloc(4096);
    assert(p != NULL);
    memset(p, 42, 4096);
    assert(p[4095] == 42);
    free(p);
    return value;
}

int main(int argc, char **argv)
{
    if (argc == 2 && (strcmp(argv[1], "spawn-ifs") == 0 || strcmp(argv[1], "wordexp-ifs") == 0)) {
        assert(setenv("IFS", "NEUTERED_IFS_PROBE", 1) == 0);
        assert(setenv("NEUTERED_CHILD_ENV", "forwarded", 1) == 0);
        if (strcmp(argv[1], "spawn-ifs") == 0) {
            char *command[] = {"sh", "-c", "exit 0", NULL};
            pid_t child;
            int status;
            assert(posix_spawn(&child, "/bin/sh", NULL, NULL, command, environ) == 0);
            assert(waitpid(child, &status, 0) == child);
            assert(WIFEXITED(status) && WEXITSTATUS(status) == 0);
        } else {
            wordexp_t words;
            assert(wordexp("$(printf marker)", &words, 0) == 0);
            assert(words.we_wordc == 1 && strcmp(words.we_wordv[0], "marker") == 0);
            wordfree(&words);
        }
        return 0;
    }
    if (argc == 2 && strcmp(argv[1], "command-path") == 0) {
        char *command[] = {"true", NULL};
        assert(setenv("PATH", "/nonexistent/narrow", 1) == 0);
        assert(execvp(command[0], command) == -1 && errno == ENOENT);
        puts("implicit command lookup denied");
        return 0;
    }
    if (argc == 3 && strcmp(argv[1], "exec-explicit") == 0) {
        char *command[] = {"policy", "exec-target", NULL};
        execvp(argv[2], command);
        return 1;
    }
    if (argc == 2 && strcmp(argv[1], "exec-target") == 0) {
        puts("explicit exec passed");
        return 0;
    }
    if (argc == 3 && (strcmp(argv[1], "load") == 0 || strcmp(argv[1], "load-lazy") == 0)) {
        int mode = strcmp(argv[1], "load-lazy") == 0 ? RTLD_LAZY : RTLD_NOW;
        void *handle = dlopen(argv[2], mode | RTLD_LOCAL);
        if (handle == NULL) {
            fprintf(stderr, "%s\n", dlerror());
            return 1;
        }
        dlclose(handle);
        return 0;
    }
    if (argc == 2 && strcmp(argv[1], "secure") == 0) {
        assert(getauxval(AT_SECURE) != 0);
        assert(getenv("NEUTERED_PUBLIC") != NULL);
        assert(secure_getenv("NEUTERED_PUBLIC") == NULL);
        const char *removed[] = {
            "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "LD_DEBUG", "LD_DEBUG_OUTPUT",
            "LD_PROFILE", "LD_PROFILE_OUTPUT", "GLIBC_TUNABLES", "GCONV_PATH", "LOCPATH",
            "HOSTALIASES", "RES_OPTIONS", "TMPDIR", "MALLOC_ARENA_MAX"
        };
        for (size_t i = 0; i < sizeof(removed)/sizeof(removed[0]); ++i)
            assert(getenv(removed[i]) == NULL);
        puts("secure policy passed");
        return 0;
    }
    if (argc == 2 && strcmp(argv[1], "nss") == 0) {
        /* Exercise a restored module name after loader startup. */
        assert(__nss_configure_lookup("passwd", "neutered_probe") == 0);
        assert(getpwnam("neutered_missing") == NULL);
        assert(getpwnam("neutered_missing") == NULL);
        return 0;
    }
    assert(getenv("NEUTERED_PUBLIC") != NULL);
    assert(strcmp(getenv("NEUTERED_PUBLIC"), "application-value") == 0);
    assert(secure_getenv("NEUTERED_PUBLIC") != NULL);
    /* Exercise the public calling convention flagged by ABI dump tooling. */
    char copied[16];
    char *(*volatile copy)(char *, const char *) = stpcpy;
    assert(copy(copied, "hello") == copied + 5);
    assert(strcmp(copied, "hello") == 0);
    assert(setenv("NEUTERED_MUTATION", "later", 1) == 0);
    assert(strcmp(getenv("NEUTERED_MUTATION"), "later") == 0);
    assert(putenv("NEUTERED_PUTENV=works") == 0);
    assert(strcmp(getenv("NEUTERED_PUTENV"), "works") == 0);
    assert(unsetenv("NEUTERED_MUTATION") == 0);
    assert(getenv("NEUTERED_MUTATION") == NULL);
    char *option_args[] = {"policy", "operand", "-p", NULL};
    assert(setenv("POSIXLY_CORRECT", "1", 1) == 0);
    optind = 0;
    assert(getopt(3, option_args, "p") == -1);
    assert(fnmatch("[^a]", "b", 0) == FNM_NOMATCH);
    assert(fnmatch("[!a]", "b", 0) == 0);
    assert(setenv("TMPDIR", "/nonexistent/private", 1) == 0);
    errno = 0;
    assert(tempnam(NULL, "neut") == NULL && errno == ENOENT);
    const char *converters = getenv("NEUTERED_CONVERTERS");
    assert(converters != NULL);
    assert(setenv("GCONV_PATH", converters, 1) == 0);
    assert(strcmp(setlocale(LC_ALL, ""), "C") == 0);
    assert(setenv("LANG", "neutered/nonexistent", 1) == 0);
    assert(setenv("LC_ALL", "neutered/nonexistent", 1) == 0);
    assert(strcmp(setlocale(LC_ALL, ""), "C") == 0);

    time_t when = 1700000000;
    struct tm baseline, changed;
    assert(localtime_r(&when, &baseline) != NULL);
    assert(setenv("TZ", "GMT+11", 1) == 0);
    tzset();
    assert(localtime_r(&when, &changed) != NULL);
    assert(baseline.tm_gmtoff == changed.tm_gmtoff);

    assert(res_init() == 0);
    unsigned long options = _res.options;
    int retry = _res.retry;
    assert(setenv("RES_OPTIONS", "rotate attempts:1", 1) == 0);
    assert(res_init() == 0);
    assert(_res.options == options && _res.retry == retry);

    iconv_t cd = iconv_open("ASCII", "UTF-8");
    assert(cd != (iconv_t)-1);
    char input[] = "hello", output[16];
    char *in = input, *out = output;
    size_t inleft = 5, outleft = sizeof(output);
    assert(iconv(cd, &in, &inleft, &out, &outleft) == 0);
    assert(inleft == 0 && memcmp(output, "hello", 5) == 0);
    assert(iconv_close(cd) == 0);
    const char *disabled[] = {"ISO-2022-CN-EXT", "IBM1390", "UTF-16", "NEUTERED-PROBE"};
    for (size_t i = 0; i < sizeof(disabled)/sizeof(disabled[0]); ++i) {
        errno = 0;
        assert(iconv_open(disabled[i], "UTF-8") == (iconv_t)-1);
        assert(errno == EINVAL);
    }
    assert(setenv("GCONV_PATH", converters, 1) == 0);
    assert(iconv_open("NEUTERED-PROBE", "UTF-8") == (iconv_t)-1);
    /* Exercise the image's real configuration, not a forced provider. */
    assert(getpwnam("root") != NULL);
    assert(getpwuid(0) != NULL);
    struct addrinfo *addresses;
    assert(getaddrinfo("127.0.0.1", NULL, NULL, &addresses) == 0);
    freeaddrinfo(addresses);
    pthread_t worker;
    assert(pthread_create(&worker, NULL, thread, (void *)42) == 0);
    void *result;
    assert(pthread_join(worker, &result) == 0 && result == (void *)42);

    void *p = malloc(256);
    assert(p != NULL);
    free(p);
    assert(clearenv() == 0);
    assert(getenv("NEUTERED_PUBLIC") == NULL);
    assert(setenv("NEUTERED_PUBLIC", "restored", 1) == 0);
    assert(strcmp(getenv("NEUTERED_PUBLIC"), "restored") == 0);
    puts("policy passed");
    return 0;
}
