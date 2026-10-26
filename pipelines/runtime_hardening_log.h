/* SPDX-License-Identifier: MIT
 * Keep the copies carried by each runtime's logging patch identical.
 * stderr is inherited from the supervisor (Werewolf sends it to the console).
 * No interpreter callbacks, application logging settings or syslog required.
 */
#ifndef RUNTIME_HARDENING_LOG_H
#define RUNTIME_HARDENING_LOG_H
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static inline char *rh_log_quote(char *out, const char *text, size_t size,
                                 size_t limit, int *truncated) {
    static const char hex[] = "0123456789abcdef";
    size_t i;
    if (size > limit) { size = limit; *truncated = 1; }
    *out++ = '"';
    for (i = 0; i < size; i++) {
        unsigned char c = (unsigned char)text[i];
        if (c == '"' || c == '\\') { *out++ = '\\'; *out++ = (char)c; }
        else if (c < 32 || c > 126) {
            memcpy(out, "\\u00", 4); out += 4;
            *out++ = hex[c >> 4]; *out++ = hex[c & 15];
        } else { *out++ = (char)c; }
    }
    *out++ = '"';
    return out;
}

/* The limits below keep the entire escaped record below PIPE_BUF (4096 on
 * Linux). One write prevents concurrent workers from interleaving records.
 * Never read the environment or the contents of a serialized payload.
 */
static inline void runtime_hardening_denied(const char *runtime, const char *group,
                                           const char *operation, const char *detail) {
    int saved_errno = errno, truncated = 0, fd;
    char line[4096], cmdline[257], *p = line;
    ssize_t n = -1, result;
    size_t i, start, length = 0;
    if (!detail) detail = "";
    p += snprintf(p, 256,
        "{\"event\":\"runtime-denial\",\"time\":%lld,\"pid\":%ld,\"euid\":%ld,\"runtime\":",
        (long long)time(NULL), (long)getpid(), (long)geteuid());
    p = rh_log_quote(p, runtime, strlen(runtime), 16, &truncated);
    memcpy(p, ",\"group\":", 9); p += 9;
    p = rh_log_quote(p, group, strlen(group), 24, &truncated);
    memcpy(p, ",\"operation\":", 13); p += 13;
    p = rh_log_quote(p, operation, strlen(operation), 48, &truncated);
    memcpy(p, ",\"detail\":", 10); p += 10;
    p = rh_log_quote(p, detail, strlen(detail), 192, &truncated);
    memcpy(p, ",\"argv\":[", 9); p += 9;
    fd = open("/proc/self/cmdline", O_RDONLY | O_CLOEXEC);
    if (fd >= 0) { n = read(fd, cmdline, sizeof(cmdline)); close(fd); }
    if (n > 0) {
        length = (size_t)n;
        if (length > 256) { length = 256; truncated = 1; }
    }
    for (i = 0, start = 0; i < length; i++) {
        if (cmdline[i] == '\0' || i + 1 == length) {
            size_t end = cmdline[i] == '\0' ? i : i + 1;
            if (start) *p++ = ',';
            p = rh_log_quote(p, cmdline + start, end - start, 256, &truncated);
            start = i + 1;
        }
    }
    p += snprintf(p, 32, "],\"truncated\":%s}\n", truncated ? "true" : "false");
    do { result = write(STDERR_FILENO, line, (size_t)(p - line)); }
    while (result < 0 && errno == EINTR);
    errno = saved_errno;
}
#endif
