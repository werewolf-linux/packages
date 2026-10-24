#define _GNU_SOURCE
#include <assert.h>
#include <netdb.h>
#include <pthread.h>
#include <pwd.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static volatile unsigned long checksum;

static double seconds(void)
{
    struct timespec now;
    assert(clock_gettime(CLOCK_MONOTONIC, &now) == 0);
    return now.tv_sec + now.tv_nsec / 1e9;
}

static void *allocate(void *unused)
{
    (void)unused;
    for (size_t i = 0; i < 2000000; ++i) {
        size_t size = 32 + (i % 2048);
        unsigned char *p = malloc(size);
        assert(p != NULL);
        memset(p, (int)i, size);
        /* Force real calls and memory access rather than compiler elision. */
        __asm__ volatile("" : : "r"(p) : "memory");
        assert(p[size - 1] == (unsigned char)i);
        free(p);
    }
    return NULL;
}

int main(int argc, char **argv)
{
    assert(argc == 2);
    if (strcmp(argv[1], "startup") == 0)
        return 0;
    double start = seconds();
    if (strcmp(argv[1], "allocate") == 0) {
        allocate(NULL);
    } else if (strcmp(argv[1], "threads") == 0) {
        pthread_t threads[4];
        for (size_t i = 0; i < 4; ++i)
            assert(pthread_create(&threads[i], NULL, allocate, NULL) == 0);
        for (size_t i = 0; i < 4; ++i)
            assert(pthread_join(threads[i], NULL) == 0);
    } else if (strcmp(argv[1], "memory") == 0) {
        size_t size = 256 * 1024;
        unsigned char *a = malloc(size), *b = malloc(size);
        assert(a && b);
        for (size_t i = 0; i < 10000; ++i) {
            memset(a, (int)i, size);
            memcpy(b, a, size);
            assert(memcmp(a, b, size) == 0);
            checksum += b[i % size];
        }
        free(a); free(b);
    } else if (strcmp(argv[1], "identity") == 0) {
        for (size_t i = 0; i < 10000; ++i) {
            struct passwd *user = getpwuid(0);
            assert(user && user->pw_uid == 0);
        }
    } else if (strcmp(argv[1], "hosts-file") == 0) {
        /* Measures the files path, not external DNS or a resolver cache. */
        struct addrinfo hints = {.ai_family = AF_INET, .ai_socktype = SOCK_STREAM};
        for (size_t i = 0; i < 10000; ++i) {
            struct addrinfo *result;
            assert(getaddrinfo("localhost", NULL, &hints, &result) == 0);
            freeaddrinfo(result);
        }
    } else {
        return 1;
    }
    printf("%.9f\n", seconds() - start);
    return 0;
}
