#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include "_cgo_export.h"
#include "disk_access_darwin.h"
#include "panel_keys_darwin.h"

@class FarshMacDelegate;
@interface FarshMacWindow : NSObject <NSWindowDelegate,WKScriptMessageHandler,WKNavigationDelegate>
@property NSWindow *window;
@property WKWebView *web;
@property NSTimer *timer;
@property BOOL ready;
@property BOOL evaluating;
@property BOOL panelKeys;
@property BOOL terminalMode;
@property BOOL geometryTransition;
@property NSRect restoredFrame;
- (void)captureFrame;
@property int session;
@property(weak) FarshMacDelegate *owner;
- (void)open;
- (void)send:(NSString *)json;
- (void)close;
@end
@interface FarshMacWebView : WKWebView
@property(weak) FarshMacWindow *controller;
- (BOOL)handlePanelOptionKey:(NSEvent *)event;
@end
@interface FarshMacDelegate : NSObject <NSApplicationDelegate,NSMenuItemValidation>
@property NSMutableArray<FarshMacWindow *> *controllers;
@property NSString *entry;
@property(nonatomic,strong) dispatch_queue_t engineQueue;
@property BOOL stopping;
@property id panelKeyMonitor;
- (void)newWindowFrom:(int)parent;
- (void)closeWindow:(FarshMacWindow *)controller;
- (void)finishQuit;
- (void)pasteText:(id)sender;
- (void)focusSession:(int)session;
- (void)diskAccessSettings:(id)sender;
- (void)startWindows;
- (void)terminalHistory:(id)sender;
- (void)terminalOutputHistory:(id)sender;
@end
@implementation FarshMacWebView
// Decode Option shortcuts using the current input layout without Option.
// charactersIgnoringModifiers still carries Option transformations on macOS.
- (BOOL)handlePanelOptionKey:(NSEvent *)event {
    NSDictionary *msg=FarshMacPanelOptionMessage(event,self.controller.panelKeys);
    if(!msg)return NO;
    NSData *json=[NSJSONSerialization dataWithJSONObject:msg options:0 error:nil];
    [self.controller send:[[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding]];
    return YES;
}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    NSEventModifierFlags flags=event.modifierFlags;
    // Option must use the current keyboard layout's base character, not the
    // macOS accent/dead-key result. Do not touch Option keys in the terminal/editor.
    NSString *s=event.charactersIgnoringModifiers;
    if(self.controller.panelKeys && ((event.keyCode==46 && (flags & (NSEventModifierFlagCommand|NSEventModifierFlagControl))) || event.keyCode==67 || event.keyCode==69 || event.keyCode==78)) {
        NSString *key=event.keyCode==46?@"m":event.keyCode==67?@"*":event.keyCode==69?@"+":@"-";
        NSString *code=event.keyCode==46?@"KeyM":event.keyCode==67?@"NumpadMultiply":event.keyCode==69?@"NumpadAdd":@"NumpadSubtract";
        NSDictionary *msg=@{@"Type":@"key",@"Key":key,@"Code":code,@"Meta":((flags & NSEventModifierFlagCommand) ? @YES : @NO),@"Ctrl":((flags & NSEventModifierFlagControl) ? @YES : @NO),@"Alt":((flags & NSEventModifierFlagOption) ? @YES : @NO),@"Shift":((flags & NSEventModifierFlagShift) ? @YES : @NO)};
        NSData *json=[NSJSONSerialization dataWithJSONObject:msg options:0 error:nil];
        [self.controller send:[[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding]];return YES;
    }

    if(self.controller.panelKeys && event.keyCode==47 && (flags & NSEventModifierFlagCommand) && (flags & NSEventModifierFlagShift) && !(flags & (NSEventModifierFlagControl|NSEventModifierFlagOption))) {
        [self.controller send:@"{\"Type\":\"key\",\"Key\":\".\",\"Code\":\"Period\",\"Meta\":true,\"Shift\":true}"]; return YES;
    }
    if(self.controller.terminalMode && (flags & NSEventModifierFlagCommand) && (flags & NSEventModifierFlagShift) && !(flags & (NSEventModifierFlagControl|NSEventModifierFlagOption)) && (event.keyCode==4 || [s.lowercaseString isEqualToString:@"h"])) {
        [self.controller.owner terminalHistory:nil]; return YES;
    }
    if ([self handlePanelOptionKey:event]) return YES;
    return [super performKeyEquivalent:event];
}
@end
@implementation FarshMacWindow
- (void)open {
    self.geometryTransition=YES;
    WKWebViewConfiguration *config=[WKWebViewConfiguration new];
    [config.userContentController addScriptMessageHandler:self name:@"farshmac"];
    config.websiteDataStore=[WKWebsiteDataStore nonPersistentDataStore];
    FarshMacWebView *web=[[FarshMacWebView alloc] initWithFrame:NSMakeRect(0,0,1120,740) configuration:config];web.controller=self;self.web=web;self.web.navigationDelegate=self;
    self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,1120,740) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable|NSWindowStyleMaskMiniaturizable backing:NSBackingStoreBuffered defer:NO];
    self.window.releasedWhenClosed=NO;
    self.window.title=[NSString stringWithFormat:@"FarshMac — %d",self.session];self.window.contentMinSize=NSMakeSize(700,400);
    self.window.contentView=self.web;self.window.delegate=self;
    self.window.backgroundColor=[NSColor colorWithSRGBRed:0 green:0 blue:0.5 alpha:1];
    if(self.restoredFrame.size.width>0){
        NSRect frame=self.restoredFrame;NSScreen *target=NSScreen.mainScreen;CGFloat bestArea=0;
        for(NSScreen *screen in NSScreen.screens){NSRect overlap=NSIntersectionRect(frame,screen.visibleFrame);CGFloat area=overlap.size.width*overlap.size.height;if(area>bestArea){bestArea=area;target=screen;}}
        NSRect bounds=target.visibleFrame;
        frame.size.width=MIN(MAX(frame.size.width,700),bounds.size.width);frame.size.height=MIN(MAX(frame.size.height,428),bounds.size.height);
        frame.origin.x=MAX(NSMinX(bounds),MIN(frame.origin.x,NSMaxX(bounds)-frame.size.width));
        frame.origin.y=MAX(NSMinY(bounds),MIN(frame.origin.y,NSMaxY(bounds)-frame.size.height));
        [self.window setFrame:frame display:NO];
    }else{[self.window center];[self.window cascadeTopLeftFromPoint:NSMakePoint(100+24*self.session,850-24*self.session)];}
    self.geometryTransition=NO;[self captureFrame];
    [self.window makeKeyAndOrderFront:nil];[self.window makeFirstResponder:self.web];
    NSURL *url=[NSURL fileURLWithPath:self.owner.entry];[self.web loadFileURL:url allowingReadAccessToURL:[url URLByDeletingLastPathComponent]];
    self.timer=[NSTimer timerWithTimeInterval:1.0/30 target:self selector:@selector(tick:) userInfo:nil repeats:YES];[[NSRunLoop mainRunLoop] addTimer:self.timer forMode:NSRunLoopCommonModes];
}
- (void)captureFrame {
    if(self.geometryTransition||self.owner.stopping||(self.window.styleMask&NSWindowStyleMaskFullScreen))return;
    NSRect f=self.window.frame;int session=self.session;
    dispatch_async(self.owner.engineQueue, ^{goWindowFrame(session,f.origin.x,f.origin.y,f.size.width,f.size.height);});
}
- (void)windowDidMove:(NSNotification *)note {[self captureFrame];}
- (void)windowDidResize:(NSNotification *)note {[self captureFrame];}
- (void)windowDidEndLiveResize:(NSNotification *)note {[self captureFrame];}
- (void)windowDidBecomeKey:(NSNotification *)note {int session=self.session;dispatch_async(self.owner.engineQueue, ^{goWindowFocused(session);});}
- (void)windowWillEnterFullScreen:(NSNotification *)note {[self captureFrame];self.geometryTransition=YES;}
- (void)windowDidExitFullScreen:(NSNotification *)note {self.geometryTransition=NO;[self captureFrame];}
- (void)windowDidFailToEnterFullScreen:(NSWindow *)window {self.geometryTransition=NO;}
- (void)send:(NSString *)json { int session=self.session;dispatch_async(self.owner.engineQueue, ^{goMessage(session,(char *)json.UTF8String);}); }
- (void)webView:(WKWebView *)web didFinishNavigation:(WKNavigation *)navigation {self.ready=YES;[self send:@"{\"Type\":\"redraw\"}"];}
- (void)webViewWebContentProcessDidTerminate:(WKWebView *)web {self.ready=NO;self.evaluating=NO;[self.web reload];}
- (void)webView:(WKWebView *)web decidePolicyForNavigationAction:(WKNavigationAction *)action decisionHandler:(void (^)(WKNavigationActionPolicy))handler {
    BOOL allowed=action.request.URL.isFileURL&&[action.request.URL.path isEqualToString:self.owner.entry];handler(allowed?WKNavigationActionPolicyAllow:WKNavigationActionPolicyCancel);
}
- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
    if (!message.frameInfo.isMainFrame||![message.body isKindOfClass:[NSDictionary class]])return;
    if ([message.body[@"Type"] isEqual:@"clipboard"]&&[message.body[@"Text"] isKindOfClass:[NSString class]]) {[[NSPasteboard generalPasteboard] clearContents];[[NSPasteboard generalPasteboard] setString:message.body[@"Text"] forType:NSPasteboardTypeString];return;}
    if ([message.body[@"Type"] isEqual:@"pasteRequest"]) {[self.owner pasteText:nil];return;}
    if ([message.body[@"Type"] isEqual:@"minSize"]) {
        // Keep the 60x15 cell minimum reachable at large font sizes.
        CGFloat w=MAX(700,[message.body[@"W"] doubleValue]),h=MAX(400,[message.body[@"H"] doubleValue]);
        self.window.contentMinSize=NSMakeSize(w,h);
        NSSize size=[self.window contentRectForFrameRect:self.window.frame].size;
        if(!(self.window.styleMask&NSWindowStyleMaskFullScreen)&&(size.width<w||size.height<h))[self.window setContentSize:NSMakeSize(MAX(w,size.width),MAX(h,size.height))];
        return;
    }
    if ([message.body[@"Type"] isEqual:@"frameDone"]) {self.evaluating=NO;if([message.body[@"More"] boolValue])[self tick:nil];return;}
    NSData *json=[NSJSONSerialization dataWithJSONObject:message.body options:0 error:nil];if(json)[self send:[[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding]];
}
- (void)tick:(NSTimer *)timer {
    if(!self.ready||self.evaluating||self.owner.stopping)return;self.evaluating=YES;
    dispatch_async(self.owner.engineQueue, ^{
        char *raw=goPoll(self.session);NSString *json=[NSString stringWithUTF8String:raw];free(raw);
        dispatch_async(dispatch_get_main_queue(), ^{
            if(!self.ready||self.owner.stopping)return;
            NSDictionary *state=[NSJSONSerialization JSONObjectWithData:[json dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];
            if([state[@"title"] isKindOfClass:[NSString class]])self.window.title=[NSString stringWithFormat:@"FarshMac — %d — %@",self.session,state[@"title"]];
            if([state[@"quitApplication"] boolValue]){[self.owner finishQuit];return;}
            if([state[@"quit"] boolValue]){[self.owner closeWindow:self];return;}
            if([state[@"newWindow"] boolValue])[self.owner newWindowFrom:self.session];
            if([state[@"attentionWindow"] intValue]) [self.owner focusSession:[state[@"attentionWindow"] intValue]];
            if([state[@"fullscreen"] boolValue])[self.window toggleFullScreen:nil];
            if([state[@"zoom"] boolValue])[self.window zoom:nil];
            if([state[@"diskAccessSettings"] boolValue])[self.owner diskAccessSettings:nil];
            self.terminalMode=[state[@"mode"] isEqual:@"terminal"];
            self.panelKeys=[state[@"panelKeys"] boolValue]&&![state[@"mode"] isEqual:@"terminal"];
            [self.web evaluateJavaScript:[NSString stringWithFormat:@"window.farshmacUpdate(%@)",json] completionHandler:^(id value,NSError *error){if(error){self.evaluating=NO;NSLog(@"FarshMac UI: %@",error.localizedDescription);}}];
        });
    });
}
- (BOOL)windowShouldClose:(NSWindow *)sender {[self send:@"{\"Type\":\"close\"}"];return NO;}
- (void)close {self.ready=NO;[self.timer invalidate];[self.web.configuration.userContentController removeScriptMessageHandlerForName:@"farshmac"];self.web.navigationDelegate=nil;self.window.delegate=nil;[self.window close];}
@end
@implementation FarshMacDelegate
- (FarshMacWindow *)active {for(FarshMacWindow *c in self.controllers)if(c.window==NSApp.keyWindow)return c;return self.controllers.lastObject;}
- (void)applicationDidFinishLaunching:(NSNotification *)note {[self startWindows];}
- (void)startWindows {
 if(self.engineQueue)return;
 self.engineQueue=dispatch_queue_create("local.farshmac.engine",DISPATCH_QUEUE_SERIAL);self.controllers=[NSMutableArray new];
 // Intercept before AppKit key equivalents and WKContentView / input methods.
 // The monitor only handles events addressed to a file-panel window.
 self.panelKeyMonitor=[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown handler:^NSEvent *(NSEvent *event){
   NSWindow *window=event.window ?: NSApp.keyWindow;
   if([window.contentView isKindOfClass:[FarshMacWebView class]] && [(FarshMacWebView *)window.contentView handlePanelOptionKey:event])return nil;
   return event;
 }];
 NSString *iconPath=[NSBundle.mainBundle pathForResource:@"FarshMac" ofType:@"icns"];if(iconPath)NSApp.applicationIconImage=[[NSImage alloc] initWithContentsOfFile:iconPath];
 NSMenu *menu=[NSMenu new];NSMenuItem *appItem=[NSMenuItem new];[menu addItem:appItem];NSMenu *appMenu=[NSMenu new];appItem.submenu=appMenu;
 [appMenu addItemWithTitle:@"About FarshMac" action:@selector(orderFrontStandardAboutPanel:) keyEquivalent:@""];
 NSMenuItem *diskAccess=[appMenu addItemWithTitle:@"Full Disk Access…" action:@selector(diskAccessSettings:) keyEquivalent:@""];diskAccess.target=self;
 [appMenu addItem:[NSMenuItem separatorItem]];
 NSMenuItem *quit=[appMenu addItemWithTitle:@"Quit FarshMac" action:@selector(requestQuit:) keyEquivalent:@"q"];quit.target=self;
 NSMenuItem *fileItem=[NSMenuItem new];[menu addItem:fileItem];NSMenu *file=[[NSMenu alloc] initWithTitle:@"File"];fileItem.submenu=file;
 NSMenuItem *newItem=[file addItemWithTitle:@"New Window" action:@selector(newWindow:) keyEquivalent:@"n"];newItem.target=self;
 NSMenuItem *close=[file addItemWithTitle:@"Close Window" action:@selector(closeActive:) keyEquivalent:@"w"];close.target=self;
 NSMenuItem *editItem=[NSMenuItem new];[menu addItem:editItem];NSMenu *edit=[[NSMenu alloc] initWithTitle:@"Edit"];editItem.submenu=edit;
 for(NSArray *entry in @[@[@"Copy",@"c",@"copyText:"],@[@"Cut",@"x",@"cutText:"],@[@"Paste",@"v",@"pasteText:"],@[@"Select All",@"a",@"selectText:"]]){NSMenuItem *item=[edit addItemWithTitle:entry[0] action:NSSelectorFromString(entry[2]) keyEquivalent:entry[1]];item.target=self;}
 NSMenuItem *windowItem=[NSMenuItem new];[menu addItem:windowItem];NSMenu *windowMenu=[[NSMenu alloc] initWithTitle:@"Window"];windowItem.submenu=windowMenu;
 NSMenuItem *historyItem=[windowMenu addItemWithTitle:@"Command History" action:@selector(terminalHistory:) keyEquivalent:@"h"];historyItem.keyEquivalentModifierMask=NSEventModifierFlagCommand|NSEventModifierFlagShift;historyItem.target=self;
 NSMenuItem *outputItem=[windowMenu addItemWithTitle:@"Terminal Output History" action:@selector(terminalOutputHistory:) keyEquivalent:@""];outputItem.target=self;
 NSMenuItem *fullScreen=[windowMenu addItemWithTitle:@"Enter Full Screen" action:@selector(toggleFullScreenActive:) keyEquivalent:@"f"];fullScreen.keyEquivalentModifierMask=NSEventModifierFlagControl|NSEventModifierFlagCommand;fullScreen.target=self;
 [windowMenu addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
 NSMenuItem *next=[windowMenu addItemWithTitle:@"Next Window" action:@selector(nextWindow:) keyEquivalent:@"`"];next.target=self;
 NSMenuItem *previous=[windowMenu addItemWithTitle:@"Previous Window" action:@selector(previousWindow:) keyEquivalent:@"`"];previous.keyEquivalentModifierMask=NSEventModifierFlagCommand|NSEventModifierFlagShift;previous.target=self;
 [windowMenu addItemWithTitle:@"Bring All to Front" action:@selector(arrangeInFront:) keyEquivalent:@""];NSApp.windowsMenu=windowMenu;NSApp.mainMenu=menu;
 __block NSString *startup;dispatch_sync(self.engineQueue, ^{char *raw=goStartupWindows();startup=[NSString stringWithUTF8String:raw];free(raw);});
 NSArray *saved=[NSJSONSerialization JSONObjectWithData:[startup dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];int focused=0;
 for(NSDictionary *item in saved){NSDictionary *f=item[@"frame"];NSRect frame=NSMakeRect([f[@"X"] doubleValue],[f[@"Y"] doubleValue],[f[@"Width"] doubleValue],[f[@"Height"] doubleValue]);[self addWindow:[item[@"id"] intValue] frame:frame];if([item[@"focused"] boolValue])focused=[item[@"id"] intValue];}
 if(focused)[self focusSession:focused];[NSApp activateIgnoringOtherApps:YES];
}
- (void)addWindow:(int)session {[self addWindow:session frame:NSZeroRect];}
- (void)addWindow:(int)session frame:(NSRect)frame {FarshMacWindow *c=[FarshMacWindow new];c.owner=self;c.session=session;c.restoredFrame=frame;[self.controllers addObject:c];[c open];}
- (void)newWindowFrom:(int)parent {if(self.stopping)return;dispatch_async(self.engineQueue, ^{int session=goNewWindow(parent);dispatch_async(dispatch_get_main_queue(), ^{if(session&&!self.stopping)[self addWindow:session];});});}
- (void)newWindow:(id)sender {[self newWindowFrom:[self active].session];}
- (void)closeActive:(id)sender {[[self active] send:@"{\"Type\":\"close\"}"];}
- (void)toggleFullScreenActive:(id)sender {[[self active].window toggleFullScreen:sender];}
- (void)diskAccessSettings:(id)sender {farshmac_open_disk_access();}
- (void)terminalOutputHistory:(id)sender {[[self active].web evaluateJavaScript:@"window.farshmacToggleOutputHistory()" completionHandler:nil];}
- (void)terminalHistory:(id)sender {[[self active].web evaluateJavaScript:@"window.farshmacToggleHistory()" completionHandler:nil];}
- (BOOL)validateMenuItem:(NSMenuItem *)item {
 if((item.action==@selector(terminalHistory:) || item.action==@selector(terminalOutputHistory:)))return [self active].terminalMode;
 if(item.action==@selector(toggleFullScreenActive:)){
  NSWindow *window=[self active].window;
  item.title=(window.styleMask&NSWindowStyleMaskFullScreen)?@"Exit Full Screen":@"Enter Full Screen";
  return window!=nil;
 }
 return YES;
}
- (void)cycle:(NSInteger)delta {if(!self.controllers.count)return;NSUInteger i=[self.controllers indexOfObject:[self active]];NSInteger next=((NSInteger)i+delta+self.controllers.count)%self.controllers.count;[self.controllers[next].window makeKeyAndOrderFront:nil];}
- (void)nextWindow:(id)sender {[self cycle:1];}
- (void)previousWindow:(id)sender {[self cycle:-1];}
- (void)focusSession:(int)session {for(FarshMacWindow *c in self.controllers)if(c.session==session&&(NSApp.keyWindow!=c.window||c.window.miniaturized)){if(c.window.miniaturized)[c.window deminiaturize:nil];[c.window makeKeyAndOrderFront:nil];break;}}
- (void)copyText:(id)sender {[[self active].web evaluateJavaScript:@"window.farshmacCopy()" completionHandler:nil];}
- (void)cutText:(id)sender {[[self active].web evaluateJavaScript:@"window.farshmacCut()" completionHandler:nil];}
- (void)selectText:(id)sender {[[self active].web evaluateJavaScript:@"window.farshmacSelectAll()" completionHandler:nil];}
- (void)pasteText:(id)sender {NSString *value=[[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];if(!value)return;NSData *data=[NSJSONSerialization dataWithJSONObject:@[value] options:0 error:nil];NSString *json=[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];[[self active].web evaluateJavaScript:[NSString stringWithFormat:@"window.farshmacPaste((%@)[0])",json] completionHandler:nil];}
- (void)closeWindow:(FarshMacWindow *)c {int session=c.session;[c close];[self.controllers removeObject:c];dispatch_async(self.engineQueue, ^{goCloseWindow(session);});}
- (void)requestQuit:(id)sender {if(!self.controllers.count){[self finishQuit];return;}dispatch_async(self.engineQueue, ^{goQuitAll();});}
- (void)finishQuit {if(self.stopping)return;for(FarshMacWindow *c in self.controllers)[c captureFrame];dispatch_async(self.engineQueue, ^{goSaveSession();});self.stopping=YES;for(FarshMacWindow *c in [self.controllers copy])[self closeWindow:c];[NSApp stop:nil];NSEvent *wake=[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];[NSApp postEvent:wake atStart:NO];}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {[self requestQuit:nil];return NSTerminateCancel;}
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {return NO;}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {if(!flag)[self newWindowFrom:0];return YES;}
@end
void farshmac_run(const char *path){@autoreleasepool{
 [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];FarshMacDelegate *delegate=[FarshMacDelegate new];delegate.entry=[NSString stringWithUTF8String:path];NSApp.delegate=delegate;
 // The preflight NSAlert may already have finished AppKit's launch before the
 // engine delegate existed. Initialise once when the main loop starts either way.
 dispatch_async(dispatch_get_main_queue(), ^{[delegate startWindows];});
 [NSApp run];
 for(FarshMacWindow *c in [delegate.controllers copy])[delegate closeWindow:c];dispatch_sync(delegate.engineQueue,^{});
}}
