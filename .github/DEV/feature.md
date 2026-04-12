filedo D: test 1000 - means in 1000 files for 95%
filedo D: test N - means in N files for 95%
Default N = 100

filedo D: probe - fast fake capacity probe via raw device I/O (~1 min)
Requires Administrator. No files written to filesystem.
Writes 32 unique markers at evenly spaced LBA offsets (0..95% of disk),
then reads them back. A fake controller remaps addresses, so markers mismatch.