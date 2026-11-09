#import "../panel_keys_darwin.h"
#include <assert.h>
int main(int argc, char **argv){@autoreleasepool{
 // Physical ANSI key positions: a, e (Option dead key), dot, minus, Shift-minus.
 NSArray *cases=@[@[@0,@NO,@"a"],@[@14,@NO,@"e"],@[@47,@NO,@"."],@[@27,@NO,@"-"],@[@27,@YES,@"_"]];
 for(NSArray *test in cases){
  CGEventRef cg=CGEventCreateKeyboardEvent(NULL,[test[0] unsignedShortValue],true);
  CGEventSetFlags(cg,kCGEventFlagMaskAlternate|([test[1] boolValue]?kCGEventFlagMaskShift:0));
  NSEvent *event=[NSEvent eventWithCGEvent:cg];
  NSDictionary *message=FarshMacPanelOptionMessage(event,YES);
  // These character expectations apply to the US layout used by CI. On other
  // layouts the decoder must match that layout's base character, not US keys.
  NSString *base=[event charactersByApplyingModifiers:([test[1] boolValue]?NSEventModifierFlagShift:0)];
  assert([message[@"Key"] isEqual:base]);
  assert([message[@"Alt"] boolValue]);
  // Go's Message fields are bool: NSNumber(int 0/1) silently drops the
  // entire key event at json.Unmarshal. Check the serialized transport type.
  NSData *json=[NSJSONSerialization dataWithJSONObject:message options:0 error:nil];
  NSDictionary *decoded=[NSJSONSerialization JSONObjectWithData:json options:0 error:nil];
  assert(CFGetTypeID((__bridge CFTypeRef)decoded[@"Alt"])==CFBooleanGetTypeID());
  assert(CFGetTypeID((__bridge CFTypeRef)decoded[@"Shift"])==CFBooleanGetTypeID());
  assert([decoded[@"Shift"] boolValue]==[test[1] boolValue]);
  if(argc>1)printf("%s\n",[[[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding] UTF8String]);
  assert(FarshMacPanelOptionMessage(event,NO)==nil);
  CGEventSetFlags(cg,kCGEventFlagMaskAlternate|kCGEventFlagMaskCommand);
  assert(FarshMacPanelOptionMessage([NSEvent eventWithCGEvent:cg],YES)==nil);
  CFRelease(cg);
 }
 if(argc==1)puts("Native Option: letters, dead keys, dot, minus, Shift-minus and scope: OK");
}}
