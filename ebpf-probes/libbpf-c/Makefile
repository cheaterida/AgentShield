# 可选：与 Aya 探针并存的 libbpf 经典构建链（需内核头文件与 clang/llvm）。
BPF_CLANG ?= clang
BPF_CFLAGS ?= -O2 -g -target bpf

.PHONY: all clean
all:
	@echo "add *.bpf.c targets; output .o for bpftool load or CO-RE skeleton"

clean:
	rm -f *.o
