	.section	.rodata.1,"aMS",@progbits,1
msg:
	.string	"This image is designed to be run as a confidential workload using libkrun.\n"
	.section	.text._start,"ax",@progbits
	.globl	_start
	.type	_start,@function
_start:
	li	a7, 64			# syscall=write
	li	a0, 2			# fd=stderr_fileno
	lui	a5, %hi(msg)		# message address
	addi	a1, a5, %lo(msg)	# message address
	li	a2, 75			# message length
	ecall
	li	a7, 93			# syscall=exit
	li	a0, 1 			# status=1
	ecall
	.section	.note.GNU-stack,"",@progbits
