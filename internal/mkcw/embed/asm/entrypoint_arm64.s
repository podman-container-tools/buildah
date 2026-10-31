	.arch armv8-a
	.section	.rodata.1,"aMS",@progbits,1
msg:
	.string	"This image is designed to be run as a confidential workload using libkrun.\n"
	.section	.text._start,"ax",@progbits
	.globl	_start
	.type	_start,@function
_start:
	mov	x8, 64			// syscall=write
	mov	x0, 2			// descriptor=2
	adrp	x1, msg			// message address
	add	x1, x1, #:lo12:msg	// message address
	mov	x2, 75			// message length
	svc	0
	mov	x8, 93			// syscall=exit
	mov	x0, 1			// status=1
	svc	0
	.section	.note.GNU-stack,"",@progbits
