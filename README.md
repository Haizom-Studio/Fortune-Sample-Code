# fortune-sample-code
This repo holds reference ASIC init and config code for customers who purchase Auradine Fortune ASICs.

The entry point for this code is AsicDetect() in adapter.go.

Variables and constants that need to be configured for user-specific hash boards and systems are marked
with a comment that includes "CONFIGURE"  in all caps in the device/devhdr/devhdr.go file.

Build instructions:

cd eval_miner/cmd/miner

make

(Generates a "miner" executable in the machine's native architecture, and arm64 and amd64 executables in the bin directory)
