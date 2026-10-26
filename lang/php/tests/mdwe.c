// Run the installed runtime with the same irreversible MDWE policy as leash.
#define _GNU_SOURCE
#include <stdio.h>
#include <sys/mman.h>
#include <sys/prctl.h>
#include <unistd.h>

#ifndef PR_SET_MDWE
#define PR_SET_MDWE 65
#define PR_GET_MDWE 66
#define PR_MDWE_REFUSE_EXEC_GAIN 1
#endif

int main(int argc, char **argv)
{
    if (argc < 2) return 2;
    if (prctl(PR_SET_MDWE, PR_MDWE_REFUSE_EXEC_GAIN, 0, 0, 0) != 0 ||
        prctl(PR_GET_MDWE, 0, 0, 0, 0) != PR_MDWE_REFUSE_EXEC_GAIN) {
        perror("MDWE");
        return 1;
    }
    size_t size = (size_t)sysconf(_SC_PAGESIZE);
    void *page = mmap(NULL, size, PROT_READ | PROT_WRITE,
                      MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    if (page == MAP_FAILED) return 1;
    if (mprotect(page, size, PROT_READ | PROT_EXEC) == 0) {
        fputs("MDWE allowed executable gain\n", stderr);
        return 1;
    }
    munmap(page, size);
    page = mmap(NULL, size, PROT_READ | PROT_WRITE | PROT_EXEC,
                MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
    if (page != MAP_FAILED) {
        fputs("MDWE allowed writable executable memory\n", stderr);
        return 1;
    }
    execvp(argv[1], argv + 1);
    perror("exec runtime");
    return 1;
}
