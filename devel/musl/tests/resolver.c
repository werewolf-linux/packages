#define _GNU_SOURCE
#include <arpa/inet.h>
#include <errno.h>
#include <ifaddrs.h>
#include <netdb.h>
#include <net/if.h>
#include <poll.h>
#include <signal.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <unistd.h>

static void fail(const char *what)
{
    perror(what);
    exit(1);
}

static void network(void)
{
    if (unshare(CLONE_NEWNET)) fail("isolated resolver network");
    int fd = socket(AF_INET, SOCK_DGRAM | SOCK_CLOEXEC, 0);
    if (fd < 0) fail("interface socket");
    struct ifreq request = {0};
    strcpy(request.ifr_name, "lo");
    if (ioctl(fd, SIOCGIFFLAGS, &request)) fail("loopback flags");
    request.ifr_flags |= IFF_UP;
    if (ioctl(fd, SIOCSIFFLAGS, &request)) fail("enable loopback");
    memset(&request, 0, sizeof request);
    strcpy(request.ifr_name, "lo:test");
    struct sockaddr_in address = {.sin_family = AF_INET};
    address.sin_addr.s_addr = inet_addr("10.53.1.1");
    memcpy(&request.ifr_addr, &address, sizeof address);
    if (ioctl(fd, SIOCSIFADDR, &request)) fail("test resolver address");
    close(fd);
}

static void client(int negative)
{
    struct addrinfo hints = {.ai_family = AF_INET, .ai_socktype = SOCK_STREAM};
    struct addrinfo *result = NULL;
    int rc = getaddrinfo("resolver-test.invalid", "443", &hints, &result);
    if (negative) exit(rc == EAI_NONAME ? 0 : 1);
    if (rc || !result || result->ai_family != AF_INET) exit(1);
    struct sockaddr_in *address = (void *)result->ai_addr;
    int good = address->sin_addr.s_addr == inet_addr("192.0.2.7") &&
        address->sin_port == htons(443);
    freeaddrinfo(result);
    exit(good ? 0 : 1);
}

/* Observe the client's actual binding before answering its query. The source
 * address on the datagram alone cannot distinguish ANY from a loopback bind. */
static void binding(unsigned port, unsigned expected)
{
    FILE *file = fopen("/proc/net/udp", "r");
    if (!file) fail("UDP table");
    char line[512];
    int found = 0;
    while (fgets(line, sizeof line, file)) {
        unsigned address, got;
        if (sscanf(line, "%*u: %8x:%4x", &address, &got) != 2 || got != port) continue;
        if (address != expected || found++) {
            fprintf(stderr, "unexpected DNS socket binding\n");
            exit(1);
        }
    }
    if (ferror(file) || fclose(file) || !found) fail("unobserved DNS socket");
}

static void check(const char *root, const char *resolver, int mixed, int negative)
{
    char path[4096];
    if (snprintf(path, sizeof path, "%s/etc/resolv.conf", root) >= (int)sizeof path)
        fail("resolver path");
    FILE *file = fopen(path, "w");
    if (!file) fail("resolver configuration");
    if (mixed) fprintf(file, "nameserver 127.0.0.53\n");
    fprintf(file, "nameserver %s\noptions attempts:1 timeout:1\n", resolver);
    if (fclose(file)) fail("close resolver configuration");

    int fd = socket(AF_INET, SOCK_DGRAM | SOCK_CLOEXEC, 0);
    if (fd < 0) fail("DNS server socket");
    struct sockaddr_in server = {.sin_family = AF_INET, .sin_port = htons(53)};
    if (inet_pton(AF_INET, resolver, &server.sin_addr) != 1 ||
        bind(fd, (void *)&server, sizeof server)) fail("bind DNS server");
    pid_t pid = fork();
    if (pid < 0) fail("fork resolver");
    if (!pid) {
        close(fd);
        if (chroot(root) || chdir("/")) fail("resolver chroot");
        execl("/probe", "probe", negative ? "negative" : "positive", NULL);
        fail("exec resolver");
    }

    struct pollfd ready = {.fd = fd, .events = POLLIN};
    if (poll(&ready, 1, 3000) != 1) {
        kill(pid, SIGKILL);
        waitpid(pid, NULL, 0);
        fail("DNS query deadline");
    }
    unsigned char packet[512];
    struct sockaddr_in peer;
    socklen_t length = sizeof peer;
    ssize_t size = recvfrom(fd, packet, sizeof packet - 16, 0, (void *)&peer, &length);
    if (size < 12 || length != sizeof peer || peer.sin_family != AF_INET)
        fail("DNS query");
    int local = (ntohl(server.sin_addr.s_addr) >> 24) == 127 && !mixed;
    binding(ntohs(peer.sin_port), local ? htonl(INADDR_LOOPBACK) : INADDR_ANY);
    packet[2] = 0x81;
    packet[3] = negative ? 0x83 : 0x80;
    packet[7] = negative ? 0 : 1;
    if (!negative) {
        const unsigned char answer[] = {0xc0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 7};
        memcpy(packet + size, answer, sizeof answer);
        size += sizeof answer;
    }
    if (sendto(fd, packet, size, 0, (void *)&peer, length) != size)
        fail("DNS response");
    close(fd);
    int status;
    if (waitpid(pid, &status, 0) != pid || !WIFEXITED(status) || WEXITSTATUS(status))
        fail("resolver result");
}

int main(int argc, char **argv)
{
    if (argc != 2) return 1;
    if (!strcmp(argv[1], "positive")) client(0);
    if (!strcmp(argv[1], "negative")) client(1);
    network();
    char root[4096], external[INET_ADDRSTRLEN] = "";
    if (!realpath(argv[1], root)) fail("fixture root");
    struct ifaddrs *interfaces;
    if (getifaddrs(&interfaces)) fail("interfaces");
    for (struct ifaddrs *i = interfaces; i; i = i->ifa_next) {
        if (!i->ifa_addr || i->ifa_addr->sa_family != AF_INET) continue;
        struct sockaddr_in *a = (void *)i->ifa_addr;
        if (!a->sin_addr.s_addr || (ntohl(a->sin_addr.s_addr) >> 24) == 127) continue;
        if (!inet_ntop(AF_INET, &a->sin_addr, external, sizeof external)) fail("interface address");
        break;
    }
    freeifaddrs(interfaces);
    if (!external[0]) fail("a non-loopback interface is required");
    check(root, "127.0.0.53", 0, 0);
    check(root, "127.0.0.53", 0, 1);
    check(root, external, 0, 0);
    check(root, external, 1, 0);
    puts("musl resolver: local, negative, remote and mixed resolver cases passed");
    return 0;
}
