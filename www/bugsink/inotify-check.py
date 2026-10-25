"""Exercise the installed Python inotify adapter against real kernel events."""
from pathlib import Path
import errno
import os
import tempfile

from inotify_simple import INotify, flags

with tempfile.TemporaryDirectory() as directory:
    with INotify(nonblocking=True) as watcher:
        assert not os.get_inheritable(watcher.fileno())
        wd = watcher.add_watch(directory, flags.CREATE)
        Path(directory, "event").touch()
        events = watcher.read(timeout=1000)
        assert any(e.wd == wd and e.name == "event" and e.mask & flags.CREATE for e in events)
        watcher.rm_watch(wd)
        try:
            watcher.add_watch(os.path.join(directory, "missing"), flags.CREATE)
        except OSError as error:
            assert error.errno == errno.ENOENT
        else:
            raise AssertionError("watching a missing path succeeded")
