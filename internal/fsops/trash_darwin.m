#import <Foundation/Foundation.h>
#include <string.h>

char *farshmac_trash_item(const char *path, char **result) {
    @autoreleasepool {
        *result = NULL;
        NSString *name = [NSString stringWithUTF8String:path];
        if (!name) return strdup("Invalid filename encoding");
        NSURL *url = [NSURL fileURLWithPath:name];
        NSError *error = nil;
        NSURL *destination = nil;
        if ([[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:&destination error:&error]) {
            if (destination) *result = strdup(destination.fileSystemRepresentation);
            return NULL;
        }
        return strdup((error.localizedDescription ?: @"Unable to move item to Trash").UTF8String);
    }
}
