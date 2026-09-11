	.section	.rodata.1,"aMS",@progbits,1
msg:
	.string	"OK\n"
	.section	.text._start,"ax",@progbits
	.globl	_start
	.type	_start,@function
_start:
	movq	$1, %rax	# syscall=write
	movq	$1, %rdi	# descriptor=1
	movq	$msg, %rsi	# buffer address
	movq	$3,%rdx		# buffer size
	syscall
	movq	$60, %rax	# syscall=exit
	movq	$0, %rdi	# status=0
	syscall
	.section	.note.GNU-stack,"",@progbits
