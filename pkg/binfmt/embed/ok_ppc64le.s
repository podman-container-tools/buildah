	.machine power8
	.machine altivec
	.abiversion 2
	.section	.rodata.1,"aMS",@progbits,1
	.align 2
msg:
	.string	"OK\n"
	.section	.text._start,"ax",@progbits
	.align 4
	.globl	_start
	.type	_start,@function
_start:
	li	0, 4			# syscall=write
	li	3, 1			# descriptor=1
	li	4, 0
        addis	4, 4, msg@highest	# buffer address
        addi	4, 4, msg@higher	# buffer address
	sldi	4, 4, 32
        addis	4, 4, msg@h		# buffer address
        addi	4, 4, msg@l		# buffer address
	li	5, 3			# buffer size
	sc
	li	0, 1			# syscall=exit
	li	3, 0			# status=0
	sc
	.section	.note.GNU-stack,"",@progbits
