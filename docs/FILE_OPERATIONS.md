# File-operation dialogs

FarshMac adapts the file-operation interactions from [Far Manager](https://github.com/FarGroup/FarManager/tree/b16f95d4d1986871b240e2dd319afc1ce3f396fb), especially `far/copy.cpp`, `far/delete.cpp`, and `far/farlang.templ.m4`. Attribution and the BSD license are included in `THIRD_PARTY_NOTICES.txt`.

## Existing destination

The warning shows the destination path and the new/existing size and modification time. Choose **Overwrite**, **Skip**, **Rename**, **Append**, or **Cancel**. Append is available only for two regular files. **Remember choice** applies to the remaining collisions in this operation; it is not saved as a global preference. Use Space on the checkbox, Alt+M, or the mouse.

Rename opens a **Rename** form with a **New name** field prefilled with `<stem> (copy)<ext>`; a relative name is resolved against the destination folder. Remembered rename generates the ` (copy)` candidate for every collision (appending ` (copy)` again if needed) and checks it again; it never overwrites an existing candidate. Existing directories merge; a file/directory type mismatch is reported without deleting either object. A read-only regular destination requires a separate confirmation. macOS ACLs and immutable flags remain enforced by the OS.

The prefilled single-item path means that exact destination, including an existing folder. A different existing directory, or a path ending in `/`, means a container for the selected items; a path ending in `/` that does not exist yet is created as a folder first. A symbolic link to a folder used as the destination acts as that folder and is never replaced. Copying a file onto itself does nothing and reports `cannot copy a file onto itself` in the status line. Renaming that changes only letter case works on case-insensitive volumes.

## Symbolic and hard links

Deleting, trashing, or renaming a link affects only the link. Overwriting an existing symbolic link replaces the link, not its target; a link to a folder given as the destination acts as that folder.

Local F5/F6 dialogs have a **Symbolic links** choice (remembered with the other copy options; not shown when either panel is remote or the source is an archive, where links are written as is):

- **Smart** (default) keeps every link pointing at the same object. This applies to selected links and to links inside copied or moved folders. A link is written verbatim when it is broken, absolute, or its target lies inside the items selected for this operation (their relative layout is preserved). A relative link to anything outside the selection is rewritten for its new location: as a relative path from the new link's folder to the original target (lexically cleaned; intermediate links in the link text are kept) on the same volume, or as an absolute path across volumes. The source is never changed. A same-volume move uses rename(2) and then rewrites the moved link, or the links inside a moved folder that point outside it, only when the parent folder changed; this walk is cancellable, and Retry/Skip apply to each link.
- **As is** writes the link text unchanged, so a relative link may dangle at the new place.
- **Copy contents** copies what links point to instead of the links. A broken link, or a link to one of its own enclosing folders (e.g. `d/loop -> ..`), is an error for that link only, with Retry / Skip / Skip all / Cancel. During a same-volume move the moved links are replaced by copies of their targets; the targets are not moved.

Copy keeps hard-link groups, like `rsync -H`: within one operation, names that share a file (device and inode) with an already-copied file, including names in different selected items and different parallel workers, become hard links to that copy. If the destination cannot create hard links, the file is copied normally. Cross-volume moves preserve hard-link groups the same way, but ask before splitting one.

**Alt+F6** creates a symbolic link (with **Relative path** on by default, measured from the link's folder) or a hard link to the item under the cursor. If the link name is an existing folder, the link is created inside it with the source's name, as in Far. Success reports `Link created: <name> → <target>` or `Hard link created: <name> → <source>`. Hard links to folders are refused, `..` cannot be linked, and an existing name is reported as such. The overwrite warning shows `New: symbolic link → <target>` (or `broken link`) for a link source, like the existing-file side.

## Failed operation

**Retry / Skip / Skip all / Cancel** displays the failing operation, paths, and the original filesystem/server error. Skip all affects subsequent errors only in the current operation. It is independent from Remember choice for collisions. A completed batch reports skipped items. PageUp/PageDown scroll long messages; Esc cancels the whole operation, like the Cancel button. When the operation cannot start at all (connecting to a server or opening the destination fails), only **Retry / Cancel** are offered.

Copy workers serialize questions. Retry repeats the failed file or step, not the whole batch. Once a move has published a destination, a retry of source deletion cannot repeat an append. Skipping a child during deletion retains its containing folder and continues with its siblings. Already completed items are not rolled back when the batch is cancelled.

Metadata preservation has its own Retry, Skip, Skip all, continue-anyway, and continue-for-all choices. Accepting changed metadata during a cross-volume move explicitly permits removing the source after copying.

Deletion asks for confirmation naming the single item or the number of selected items. Shift+F8/Shift+Delete always asks; the **Confirm Move to Trash** setting only controls the F8 question.

## macOS and server differences

- F8 uses native Trash; failure never silently becomes permanent deletion. Shift+F8/Shift+Delete deletes permanently.
- Read-only confirmation uses POSIX mode bits. ACL, immutable-flag, Full Disk Access, and server authorization failures appear as normal errors; the app does not bypass them or request elevated execution.
- Local same-filesystem moves use rename. Cross-volume local moves go file by file: each file is copied with its metadata, loss of metadata requires consent, and the source file is deleted only after its copy is published. Retry and Skip apply to the failing file, not the whole tree.
- Same-server remote moves merge directories and handle conflicts. If the server cannot rename or replace an entry, it reports an error. Moving between servers remains copy followed by explicit deletion.
- FTP/SFTP metadata and rename guarantees depend on the server. There is no portable atomic compare-and-swap for a remote destination. Known protocol limits on remote metadata are explained once per operation, not once per file. Replacing an existing remote file needs SFTP `posix-rename` or an FTP server that allows renaming over a file; otherwise the error says the server cannot replace the file atomically and suggests deleting it first or choosing Rename.
- Retry after a lost connection reconnects first (status `Reconnecting to <label>`) instead of reusing the dead session.
- `~` is refused in remote destinations; enter an absolute server path.
- Windows EFS decryption, NTFS alternate streams, UAC elevation, secure wipe, and Windows-plugin dialogs are not transplanted into macOS. The separate archive extractor still requires a new destination; use an archive panel and F5 for per-file merging and collision choices.
