# Original Far Manager icon

Source: [FarGroup/FarManager](https://github.com/FarGroup/FarManager), pinned to
commit `b16f95d4d1986871b240e2dd319afc1ce3f396fb`.

- [Far.ico](https://github.com/FarGroup/FarManager/blob/b16f95d4d1986871b240e2dd319afc1ce3f396fb/far/Far.ico): original Windows icon, used for the application.
- [Far.svg](https://github.com/FarGroup/FarManager/blob/b16f95d4d1986871b240e2dd319afc1ce3f396fb/misc/artwork/icon/src/svg/48.svg): original vector reference.
- [LICENSE](LICENSE): upstream BSD 3-Clause license.
- `copyright.inc.m4`: upstream copyright notice from `far/copyright.inc.m4`.

`../FarshMac.icns` contains the original artwork resized for macOS icon slots,
without redrawing it. Regenerate on macOS with `./scripts/build-icon.sh` from
the project root. The original ICO's largest image is 256×256, so larger slots
are upscaled. The license is also included in the built app's third-party notices.
