# Explicit dependencies replace preload-based instrumentation in test programs.
neutered-trace-test = $(and $(findstring MALLOC_TRACE=,$($(@F)-ENV)),$(findstring libc_malloc_debug.so,$($(@F)-ENV)))
neutered-format-test = $(filter $(@F),$(xprintf-srcs) $(xscanf-srcs))
neutered-trace-libs = -Wl,--no-as-needed $(common-objpfx)malloc/libc_malloc_debug.so -Wl,--as-needed -Wl,-rpath,$(common-objpfx)malloc
override link-extra-libs = $(LDLIBS-$(@F)) $(if $(filter $(@F),$(tests-static)),,$(if $(or $(neutered-trace-test),$(neutered-format-test)),$(neutered-trace-libs)))
ifndef link-extra-libs-static
link-extra-libs-static = $(LDLIBS-$(@F))
endif
