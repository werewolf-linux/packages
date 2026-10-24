#include <cassert>
#include <execinfo.h>
#include <stdexcept>

__attribute__((noinline)) static void throw_error()
{
    void *frames[64];
    assert(backtrace(frames, 64) >= 2);
    throw std::runtime_error("unwind probe");
}

int main()
{
    try {
        throw_error();
        return 1;
    } catch (const std::runtime_error &) {
        void *frames[64];
        assert(backtrace(frames, 64) >= 2);
        return 0;
    }
}
