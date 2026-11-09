# Archives in FarshMac 0.11

This document records the archive implementation introduced in 0.11. See the
[README](README.md) and [changelog](CHANGELOG.md) for current release information.

| Format | Create new archive | List / test / extract |
| --- | --- | --- |
| ZIP, JAR, WAR | Yes (ZIP container; no generated Java manifest) | Yes |
| TAR | Yes | Yes |
| tar.gz / tgz | Yes | Yes |
| tar.bz2 / tbz / tbz2 | Yes | Yes |
| tar.xz / txz | Yes | Yes |
| 7z | Yes | Yes |
| RAR | No | Subject to system libarchive capabilities |
| CPIO | No | Through system libarchive |

**Enter opens an archive as a file panel**, even when the archive has its executable
bit set. ZIP/JAR/WAR, TAR and compressed TAR, 7z, RAR and CPIO share the same catalog
interface. Enter or double-click opens directories. `..` returns to the parent;
at the archive root it restores the ordinary panel with the cursor on the archive.
Selection, sorting, filters, fast find and nested directory state are supported.

**F5** extracts the current file, marked files or an entire directory to the ordinary
filesystem. The destination dialog supports renaming, directory merging, conflict
confirmation, progress and Esc. Selected content and any required hard-link targets
are extracted temporarily and passed to the normal copy engine. The temporary
directory is removed on success, failure or cancellation. Cancelling a copy retains
files already completed, just like an ordinary F5 operation.

**F3**, or Enter on an ordinary archive member, opens the read-only viewer. Its
temporary copy is removed when the document/window closes. Editing, deletion, moving
out of an archive and writing changes back are not supported: first copy out with
F5. Nested archives must also be copied out before opening. Passwords and multi-volume
archives are unsupported. Restart restores the containing directory and selects the
archive, rather than restoring the temporary archive view.

F9 → Files → Archive commands also provides a text listing, a whole-archive test and
full extraction. Create archive selects the format from the new filename's extension.
Cmd+R rereads the archive catalog. Changes to the source archive are checked before
extraction and before publishing its result.

## Implementation

The catalog retains metadata only: at most 100,000 nodes, including implicit
directories; 16 MiB of names/link targets; paths up to 4096 bytes; and depth up to
64 separators. ZIP uses its central directory without decompressing payloads.
For Store/Deflate ZIP members, F5/F3 read only selected entries and verify their CRC.
This does not validate unselected entries; use Test archive for that. Other ZIP
compression methods use the system decoder.

Other formats are indexed by a background streaming pass without retaining unpacked
data. Selective extraction also scans the stream but writes only selected entries.
Esc cancels reading/extraction and removes the decoder process and temporary results.

Go's standard library creates ZIP files. For other containers, Go produces a TAR
stream for `/usr/bin/tar` (macOS bsdtar/libarchive). When reading, the system decoder
converts input to PAX/TAR; Go validates names/types and creates files. bsdtar never
receives a command to extract directly onto disk. No shell is used; paths are separate
arguments. Homebrew is unnecessary, and the application bundle does not include
third-party archivers or libarchive. Decoder capabilities depend on macOS version.
See [libarchive formats](https://github.com/libarchive/libarchive/wiki/LibarchiveFormats)
and [bsdtar](https://github.com/libarchive/libarchive/blob/master/tar/bsdtar.1).

- A new archive is prepared in a temporary file and published without replacing an existing file.
- Extraction uses a private 0700 staging directory beside the destination, followed by an atomic rename without overwrite. Failure/cancellation removes staging files.
- Absolute paths, Windows drive prefixes, backslashes and `..` traversal are rejected, as are duplicate ordinary files and special files.
- Data is written through `os.Root`. Symbolic and hard links are created after files; absolute/external links are forbidden. A hard-link target must already be an ordinary extracted file. Cyclic symbolic links are unsupported.
- Contents, ordinary POSIX permissions and file modification times are retained. Owner read/write permission is added, plus execute for directories. ACLs, xattrs, owner/group, resource forks and directory modification times are not restored.
- Extraction and reporting are limited to 100,000 entries. Reports display up to 10,000 lines/about 4 MiB and explicitly indicate truncation; remaining entries are still read and checked. The entire stream is never retained in memory or on disk.
- ZIP must have a valid central directory. Recovering local entries from a truncated ZIP is not considered success.
- Go additionally validates the complete gzip stream, including CRC and size, because the system libarchive filter does not guarantee this. This requires an extra compressed-file pass. Concatenated gzip members and zero padding are accepted.
- Esc cancels Go loops and terminates the archiver's separate process group. Until total size is known, progress is indeterminate, with processed bytes, rate and current-file progress. Archive creation knows its input total after scanning. Final compression can continue after all input bytes have been sent; cancellation remains available.

## Limitations

Password-protected and multi-volume archives are unsupported. The archiver has no
controlling terminal, so a hidden password prompt should not block the window.
RAR creation, editing existing archive entries, merging full extraction into an
existing directory, and transparent editing of archive members are not implemented.
Standalone gzip/bzip2/xz streams without TAR are outside the supported scope.
Unsupported format variants report an error.

## Validation

Integration tests use the installed macOS bsdtar. Coverage includes round trips for
ZIP/JAR/WAR, TAR, gzip/bzip2/xz TAR and abbreviated extensions, and 7z; Unicode,
spaces, quotes and newlines in names; empty directories; symbolic/hard links;
overwrite prevention; gzip CRC; corrupt archives; traversal; and staging cleanup.
RAR was tested with a minimal stored RAR4 fixture, not every compression variant.

Tests also cover cancellation of large TAR creation/extraction/testing; Enter →
archive panel → subdirectories → F5/F3 → `..`; cursor/selection restoration;
multi-file extraction; viewer cleanup; cancelled opening; shortcuts; panel
synchronization; selected ZIP reads with an unselected corrupt member; rejection of
a corrupt selected member; reports; extraction; and 7z creation through the UI model.
A full manual Cocoa workflow and execution on Intel were not performed for this audit.
