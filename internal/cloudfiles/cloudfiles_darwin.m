#import <Foundation/Foundation.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/stat.h>
#include <errno.h>
#include <string.h>

char *farshmac_cloud_materialize(const char *path, char **message) {
 @autoreleasepool {
  NSURL *url = [NSURL fileURLWithFileSystemRepresentation:path isDirectory:NO relativeToURL:nil];
  NSNumber *ubiquitous = nil;
  [url getResourceValue:&ubiquitous forKey:NSURLIsUbiquitousItemKey error:nil];
  NSError *error = nil;
  if (ubiquitous.boolValue && ![[NSFileManager defaultManager] startDownloadingUbiquitousItemAtURL:url error:&error]) {
   *message = strdup(error.localizedDescription.UTF8String);
   return NULL;
  }
  NSFileCoordinator *coordinator = [[NSFileCoordinator alloc] initWithFilePresenter:nil];
  __block NSString *resolved = nil;
  __block NSError *readError = nil;
  // Coordinated reads wait for iCloud / File Provider materialization. Reading
  // one byte in the accessor also triggers providers that hydrate on first I/O.
  [coordinator coordinateReadingItemAtURL:url options:NSFileCoordinatorReadingWithoutChanges error:&error byAccessor:^(NSURL *available) {
   int fd = open(available.fileSystemRepresentation, O_RDONLY);
   if (fd < 0) { readError = [NSError errorWithDomain:NSPOSIXErrorDomain code:errno userInfo:nil]; return; }
   struct stat st;
   int code = 0;
   if (fstat(fd, &st) != 0) code = errno;
   else if (!S_ISREG(st.st_mode)) code = EINVAL;
   else { char byte; if (pread(fd, &byte, 1, 0) < 0) code = errno; }
   close(fd);
   if (code) readError = [NSError errorWithDomain:NSPOSIXErrorDomain code:code userInfo:nil];
   else resolved = available.path;
  }];
  if (error || readError || !resolved) {
   *message = strdup((error ?: readError).localizedDescription.UTF8String ?: "Cloud file is not available");
   return NULL;
  }
  return strdup(resolved.fileSystemRepresentation);
 }
}
