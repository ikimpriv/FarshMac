#import <Cocoa/Cocoa.h>

// Translate the current layout without Option's accents / alternate symbols,
// retaining Shift and Caps Lock. Use the physical code only for keypad identity.
static NSDictionary *FarshMacPanelOptionMessage(NSEvent *event, BOOL panelKeys) {
    NSEventModifierFlags flags=event.modifierFlags;
    if (!panelKeys || event.type!=NSEventTypeKeyDown || !(flags & NSEventModifierFlagOption) || (flags & (NSEventModifierFlagCommand|NSEventModifierFlagControl))) return nil;
    NSString *s=[event charactersByApplyingModifiers:(flags & (NSEventModifierFlagShift|NSEventModifierFlagCapsLock))];
    if(!s.length)s=event.charactersIgnoringModifiers;
    if(!s.length)return nil;
    unichar ch=[s characterAtIndex:0];
    if(ch<32 || (ch>=0xF700 && ch<=0xF8FF) || ch==127)return nil;
    NSString *code=event.keyCode==67?@"NumpadMultiply":event.keyCode==69?@"NumpadAdd":event.keyCode==78?@"NumpadSubtract":@"";
    return @{@"Type":@"key",@"Key":s,@"Code":code,@"Alt":@YES,@"Shift":((flags & NSEventModifierFlagShift) ? @YES : @NO)};
}
