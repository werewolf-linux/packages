"""Build the three native libc bindings for this Python extension."""
from pathlib import Path
import sys
from cffi import FFI

site = Path(sys.argv[1])
ffi = FFI()
ffi.cdef("""
    int inotify_init1(int flags);
    int inotify_add_watch(int fd, const char *path, unsigned int mask);
    int inotify_rm_watch(int fd, int wd);
""")
ffi.set_source("_werewolf_inotify", "#include <sys/inotify.h>")
ffi.compile(tmpdir="/tmp/werewolf-inotify", target=str(site / "_werewolf_inotify.*"))
