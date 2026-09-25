/* between syscall(2), the output of cc -S on this file, the Go syscall
 * package's zsysnum_ files, and a web search, we can get pretty far */
extern long syscall(long number, ...);
int main(int arc, char **argv) {
	syscall(2, 1, "OK\n", 3);
	syscall(80, 0);
}
