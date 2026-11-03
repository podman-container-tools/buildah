	.section	.rodata.1,"aMS",@progbits,1
	.align 3
msg:
	.string	"OK\n"
	.section	.text._start,"ax",@progbits
	.align 1
	.globl	_start
	.type	_start,@function
_start:
	li	a7, 64			# syscall=write
	li	a0, 1			# descriptor=1
	lui	a5, %hi(msg)		# buffer address
	addi	a1, a5, %lo(msg)	# buffer address
	li	a2, 3			# buffer size
	ecall
	li	a7, 93			# syscall=exit
	li	a0, 0			# status=0
	ecall
	.section	.note.GNU-stack,"",@progbits
