#import "disk_access_darwin.h"

BOOL farshmac_open_disk_access(void) {
    NSWorkspace *workspace = NSWorkspace.sharedWorkspace;
    NSURL *bundle = NSBundle.mainBundle.bundleURL;
    // Select this exact copy, so the user can drag it into Full Disk Access or
    // locate it with '+'. Do not probe protected files or modify the TCC DB.
    if ([bundle.pathExtension isEqualToString:@"app"]) {
        [workspace activateFileViewerSelectingURLs:@[bundle]];
    }
    // The pane deep link is best effort; fall back to opening Settings itself.
    NSURL *pane = [NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"];
    if ([workspace openURL:pane]) return YES;
    NSURL *settings = [workspace URLForApplicationWithBundleIdentifier:@"com.apple.systempreferences"];
    if (settings && [workspace openURL:settings]) return YES;
    NSAlert *alert = [NSAlert new];
    alert.messageText = @"Open Full Disk Access in System Settings";
    alert.informativeText = @"Open Privacy & Security → Full Disk Access, add this copy of FarshMac and enable it. Then quit and reopen FarshMac.";
    [alert addButtonWithTitle:@"OK"];
    [alert runModal];
    return NO;
}

int farshmac_prepare_disk_access(void) {
    @autoreleasepool {
        // CLI/test/helper executables must not get a GUI setup prompt.
        NSBundle *bundle = NSBundle.mainBundle;
        if (![bundle.bundleIdentifier isEqualToString:@"local.farshmac.app"] ||
            ![bundle.bundleURL.pathExtension isEqualToString:@"app"]) return 1;
        NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
        NSString *key = @"FarshMacDiskAccessIntroductionShown";
        if ([defaults boolForKey:key]) return 1;
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        [NSApp activateIgnoringOtherApps:YES];
        NSAlert *alert = [NSAlert new];
        alert.messageText = @"Full Disk Access for FarshMac";
        alert.informativeText = [NSString stringWithFormat:
            @"To browse protected folders without separate folder prompts, enable FarshMac in System Settings → Privacy & Security → Full Disk Access.\n\n"
             "If it is missing, use '+' or drag the app selected in Finder:\n%@\n\n"
             "Open Settings closes this launch before folders are loaded. Enable access, then reopen FarshMac. If already enabled, choose Continue. You can also continue with limited access and use FarshMac → Full Disk Access later.", bundle.bundlePath];
        [alert addButtonWithTitle:@"Open Settings"];
        NSButton *later = [alert addButtonWithTitle:@"Continue"];
        later.keyEquivalent = @"\033";
        NSModalResponse response = [alert runModal];
        // This is only a reminder preference, never evidence of granted access.
        [defaults setBool:YES forKey:key];
        if (response == NSAlertFirstButtonReturn && farshmac_open_disk_access()) return 0;
        return 1;
    }
}
