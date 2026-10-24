# What this Python lets a program do, as one JSON line; tests/probe.sh compares
# it with what the protections in patches/series and the image's allowances say.
import importlib
import json
import pickle
import site
import sqlite3
import sys


def can_import(name):
    try:
        importlib.import_module(name)
    except ImportError:
        return False
    return True


def can_unpickle():
    blob = pickle.dumps(len)          # a GLOBAL opcode: find_class('builtins', 'len')
    try:
        pickle.loads(blob)
    except Exception:
        return False
    return True


print(json.dumps({
    'ignore_environment': sys.flags.ignore_environment,
    'no_user_site': sys.flags.no_user_site,
    'user_site': bool(site.ENABLE_USER_SITE),
    'sibling': can_import('sibling'),      # beside this script: still on sys.path
    'pythonpath': can_import('pathevil'),  # in PYTHONPATH
    'userbase': can_import('userevil'),    # in PYTHONUSERBASE's site-packages
    'sqlite_extensions': hasattr(sqlite3.Connection, 'enable_load_extension'),
    'native': can_import('ctypes'),        # python:native
    'deserialize': can_unpickle(),         # python:deserialize
}, sort_keys=True))
