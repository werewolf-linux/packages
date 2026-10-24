/* Normal IFUNC dispatch must survive removal of loader auditing. */
#define _GNU_SOURCE
#include <assert.h>
#include <stddef.h>

#ifdef IFUNC_LOAD
#include <dlfcn.h>
#include <link.h>

int
main(int argc, char **argv)
{
    assert(argc == 2);
    void *handles[] = {
        dlopen(argv[1], RTLD_NOW | RTLD_LOCAL),
        dlmopen(LM_ID_NEWLM, argv[1], RTLD_NOW | RTLD_LOCAL),
    };
    for (unsigned int i = 0; i < sizeof(handles) / sizeof(handles[0]); ++i) {
        assert(handles[i] != NULL);
        int (*value)(void) = dlsym(handles[i], "neutered_ifunc");
        assert(value != NULL && value() == 42);
        assert(dlclose(handles[i]) == 0);
    }
    return 0;
}
#else
static int
implementation(void)
{
    return 42;
}

static int (*
resolve(void))(void)
{
    return implementation;
}

int neutered_ifunc(void) __attribute__((ifunc("resolve")));

#ifndef IFUNC_DSO
int
main(void)
{
    assert(neutered_ifunc() == 42);
    return 0;
}
#endif
#endif
