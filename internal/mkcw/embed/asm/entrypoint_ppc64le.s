	.machine power8
	.machine altivec
	.abiversion 2
	.section	.rodata.1,"aMS",@progbits,1
	.align 2
msg:
	.string	"This image is designed to be run as a confidential workload using libkrun.\n"
	.section	.text._start,"ax",@progbits
	.align 4
	.globl	_start
	.type	_start,@function
_start:
	li	0, 4			# syscall=write
	li	3, 2			# descriptor=2
	li	4, 0
        addis	4, 4, msg@highest	# message address
        addi	4, 4, msg@higher	# message address
	sldi	4, 4, 32
        addis	4, 4, msg@h		# message address
        addi	4, 4, msg@l		# message address
	li	5, 75			# message length
	sc
	li	0, 1			# syscall=exit
	li	3, 1			# status=1
	sc
	.section	.note.GNU-stack,"",@progbits
