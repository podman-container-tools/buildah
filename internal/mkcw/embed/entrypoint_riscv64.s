DATA msg+0(SB)/75, $"This image is designed to be run as a confidential workload using libkrun.\n"

GLOBL msg(SB),8,$75

TEXT _start(SB),8-0,$0
	MOV	$64, A7		// syscall=write
	MOV	$2, A0		// descriptor=2
	MOV	$msg(SB), A1	// buffer (msg) address
	MOV	$75, A2		// buffer (msg) length
	ECALL
	MOV	$93, A7		// syscall=exit
	MOV	$1, A0		// status=1
	ECALL
