# File-operation dialogs

FarshMac adapts the file-operation interactions from [Far Manager](https://github.com/FarGroup/FarManager/tree/b16f95d4d1986871b240e2dd319afc1ce3f396fb), especially `far/copy.cpp`, `far/delete.cpp`, and `far/farlang.templ.m4`. Attribution and the BSD license are included in `THIRD_PARTY_NOTICES.txt`.

## Existing destination

The warning shows the destination path and the new/existing size and modification time. Choose **Overwrite**, **Skip**, **Rename**, **Append**, or **Cancel**. Append is available only for two regular files. **Remember choice** applies to the remaining collisions in this operation; it is not saved as a global preference. Use Space on the checkbox, Alt+M, or the mouse.

Rename asks for a destination path. Remembered rename generates a new candidate name per collision and checks it again. Existing directories merge; a file/directory type mismatch is reported without deleting either object. A read-only regular destination requires a separate confirmation. macOS ACLs and immutable flags remain enforced by the OS.

The prefilled single-item path means that exact destination, including an existing folder. A different existing directory, or a path ending in `/`, means a container for the selected items.

## Failed operation

**Retry / Skip / Skip all / Cancel** displays the failing operation, paths, and the original filesystem/server error. Skip all affects subsequent errors only in the current operation. It is independent from Remember choice for collisions. A completed batch reports skipped items. PageUp/PageDown scroll long messages; Esc cancels a pending decision.

Copy workers serialize questions. Retry repeats the failed file or step, not the whole batch. Once a move has published a destination, a retry of source deletion cannot repeat an append. Skipping a child during deletion retains its containing folder and continues with its siblings. Already completed items are not rolled back when the batch is cancelled.

Metadata preservation has its own Retry, Skip, Skip all, continue-anyway, and continue-for-all choices. Accepting changed metadata during a cross-volume move explicitly permits removing the source after copying.

## macOS and server differences

- F8 uses native Trash; failure never silently becomes permanent deletion. Shift+F8/Shift+Delete deletes permanently.
- Read-only confirmation uses POSIX mode bits. ACL, immutable-flag, Full Disk Access, and server authorization failures appear as normal errors; the app does not bypass them or request elevated execution.
- Local same-filesystem moves use rename. Cross-volume local moves preserve metadata or request consent for loss.
- Same-server remote moves merge directories and handle conflicts. If the server cannot rename or replace an entry, it reports an error. Moving between servers remains copy followed by explicit deletion.
- FTP/SFTP metadata and rename guarantees depend on the server. There is no portable atomic compare-and-swap for a remote destination.
- Windows EFS decryption, NTFS alternate streams, UAC elevation, secure wipe, and Windows-plugin dialogs are not transplanted into macOS. The separate archive extractor still requires a new destination; use an archive panel and F5 for per-file merging and collision choices.
