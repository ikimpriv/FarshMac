package preview

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework ImageIO -framework CoreGraphics
#import <Foundation/Foundation.h>
#import <ImageIO/ImageIO.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
static char *thumbnail(const char *filename, int maxPixels, int pageNumber) {
 @autoreleasepool {
 NSURL *url=[NSURL fileURLWithPath:[NSString stringWithUTF8String:filename]];
 CGImageRef image=NULL; int width=0,height=0,pages=0;
 NSString *format=url.pathExtension.uppercaseString;
 if ([format isEqualToString:@"PDF"]) {
  CGPDFDocumentRef doc=CGPDFDocumentCreateWithURL((__bridge CFURLRef)url);
  if(!doc)return NULL;
  pages=(int)CGPDFDocumentGetNumberOfPages(doc);
  CGPDFPageRef page=pageNumber>=1 && pageNumber<=pages?CGPDFDocumentGetPage(doc,pageNumber):NULL;
  if(page){
   CGRect box=CGPDFPageGetBoxRect(page,kCGPDFCropBox);
   if(box.size.width>0 && box.size.height>0){
    double scale=(double)maxPixels/MAX(box.size.width,box.size.height);
    int w=MAX(1,(int)(box.size.width*scale)),h=MAX(1,(int)(box.size.height*scale));
    width=(int)box.size.width;height=(int)box.size.height;
    if(CGPDFPageGetRotationAngle(page)%180){int t=w;w=h;h=t;t=width;width=height;height=t;}
    CGColorSpaceRef cs=CGColorSpaceCreateDeviceRGB();
    CGContextRef ctx=CGBitmapContextCreate(NULL,w,h,8,w*4,cs,(CGBitmapInfo)kCGImageAlphaPremultipliedLast);CGColorSpaceRelease(cs);
    if(ctx){CGContextSetRGBFillColor(ctx,1,1,1,1);CGContextFillRect(ctx,CGRectMake(0,0,w,h));// CoreGraphics fits down but does not enlarge PDF content. Map in page
    // points first, then explicitly scale those points to bitmap pixels.
    double pointW=box.size.width,pointH=box.size.height;
    if(CGPDFPageGetRotationAngle(page)%180){double t=pointW;pointW=pointH;pointH=t;}
    CGContextScaleCTM(ctx,(double)w/pointW,(double)h/pointH);
    CGContextConcatCTM(ctx,CGPDFPageGetDrawingTransform(page,kCGPDFCropBox,CGRectMake(0,0,pointW,pointH),0,true));CGContextDrawPDFPage(ctx,page);image=CGBitmapContextCreateImage(ctx);CGContextRelease(ctx);}
   }
  }
  CGPDFDocumentRelease(doc);
 } else {
  CGImageSourceRef src=CGImageSourceCreateWithURL((__bridge CFURLRef)url,(__bridge CFDictionaryRef)@{(id)kCGImageSourceShouldCache:@NO});
  if(!src)return NULL;
  NSDictionary *props=CFBridgingRelease(CGImageSourceCopyPropertiesAtIndex(src,0,NULL));
  width=[props[(id)kCGImagePropertyPixelWidth] intValue];height=[props[(id)kCGImagePropertyPixelHeight] intValue];
  int orientation=[props[(id)kCGImagePropertyOrientation] intValue];if(orientation>=5 && orientation<=8){int t=width;width=height;height=t;}
  if(width>0 && height>0 && width<=100000 && height<=100000 && (long long)width*height<=500000000){
   image=CGImageSourceCreateThumbnailAtIndex(src,0,(__bridge CFDictionaryRef)@{(id)kCGImageSourceCreateThumbnailFromImageAlways:@YES,(id)kCGImageSourceThumbnailMaxPixelSize:@(maxPixels),(id)kCGImageSourceCreateThumbnailWithTransform:@YES,(id)kCGImageSourceShouldCache:@NO});
  }
  CFRelease(src);
 }
 if(!image)return NULL;
 // Large views of photographs go out as JPEG; PNG keeps alpha for images that may have it.
 BOOL jpeg=maxPixels>768 && !([format isEqualToString:@"PNG"] || [format isEqualToString:@"GIF"] || [format isEqualToString:@"PDF"]);
 NSMutableData *data=[NSMutableData data];CGImageDestinationRef dest=CGImageDestinationCreateWithData((__bridge CFMutableDataRef)data,jpeg?CFSTR("public.jpeg"):CFSTR("public.png"),1,NULL);
 if(!dest){CGImageRelease(image);return NULL;}
 CGImageDestinationAddImage(dest,image,jpeg?(__bridge CFDictionaryRef)@{(id)kCGImageDestinationLossyCompressionQuality:@0.9}:NULL);BOOL ok=CGImageDestinationFinalize(dest);CFRelease(dest);CGImageRelease(image);
 NSUInteger limit=maxPixels>768?12*1024*1024:3*1024*1024;
 if(!ok || data.length>limit)return NULL;
 NSDictionary *result=@{@"Data":[(jpeg?@"data:image/jpeg;base64,":@"data:image/png;base64,") stringByAppendingString:[data base64EncodedStringWithOptions:0]],@"Width":@(width),@"Height":@(height),@"Pages":@(pages),@"Format":format};
 NSData *json=[NSJSONSerialization dataWithJSONObject:result options:0 error:nil];
 return json?strdup([[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding].UTF8String):NULL;
 }
}
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"
)

func decode(path string, maxPixels int, pageNumber ...int) (Result, error) {
	page := 1
	if len(pageNumber) > 0 {
		page = pageNumber[0]
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	out := C.thumbnail(p, C.int(maxPixels), C.int(page))
	if out == nil {
		return Result{}, fmt.Errorf("preview unavailable")
	}
	defer C.free(unsafe.Pointer(out))
	var r Result
	err := json.Unmarshal([]byte(C.GoString(out)), &r)
	return r, err
}
