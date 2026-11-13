	.machinemode zarch
	.machine "z13"
	.section	.rodata.1,"aMS",@progbits,1
	.align 4
msg:
	.string	"This image is designed to be run as a confidential workload using libkrun.\n"
	.section	.text._start,"ax",@progbits
	.align 4
	.globl	_start
	.type	_start,@function
_start:
	lghi	%r1, 4
	lghi	%r2, 1
        larl	%r3, msg
	lghi	%r4, 75
	svc 0
	lghi	%r1, 1
	lghi	%r2, 1
	svc 0
	.section	.note.GNU-stack,"",@progbits
