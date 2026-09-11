	.arch armv8-a
	.section	.rodata.1,"aMS",@progbits,1
msg:
	.string	"OK\n"
	.section	.text._start,"ax",@progbits
	.globl	_start
	.type	_start,@function
_start:
	mov	x8, 64			// syscall=write
	mov	x0, 1			// descriptor=1
	adrp	x1, msg			// buffer address
	add	x1, x1, #:lo12:msg	// buffer address
	mov	x2, 3			// buffer size
	svc	0
	mov	x8, 93			// syscall=exit
	mov	x0, 0			// status=0
	svc	0
	.section	.note.GNU-stack,"",@progbits
