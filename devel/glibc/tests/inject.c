#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>

int neutered_dependency(void)
{
    return 42;
}

/* A constructor gives the test observable evidence of code execution. */
__attribute__((constructor)) static void injected(void)
{
    const char *path = getenv("NEUTERED_MARKER");
    if (path != NULL) {
        int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
        if (fd >= 0) {
            if (write(fd, "injected\n", 9) != 9)
                _exit(125);
            close(fd);
        }
    }
}
