package sysinfo

/*
#cgo LDFLAGS: -framework CoreFoundation -framework IOKit
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>
#include <mach/mach.h>
#include <sys/sysctl.h>
#include <stdlib.h>
#include <string.h>
typedef struct {
 unsigned long long total, free, available, compressed, swapTotal, swapFree;
 char volume[256], uuid[256], power[64], charge[64];
 int percent, minutes;
} farshmac_system_info;
static void copy_string(CFTypeRef v,char *out,size_t n){if(v&&CFGetTypeID(v)==CFStringGetTypeID())CFStringGetCString(v,out,n,kCFStringEncodingUTF8);}
static int number(CFDictionaryRef d,CFStringRef key){int n=-1;CFNumberRef v=CFDictionaryGetValue(d,key);if(v&&CFGetTypeID(v)==CFNumberGetTypeID())CFNumberGetValue(v,kCFNumberIntType,&n);return n;}
static farshmac_system_info collect(const char *path){
 farshmac_system_info s={0};s.percent=-1;s.minutes=-1;
 size_t n=sizeof(s.total);sysctlbyname("hw.memsize",&s.total,&n,NULL,0);
 vm_statistics64_data_t vm={0};mach_msg_type_number_t count=HOST_VM_INFO64_COUNT;
 mach_port_t host=mach_host_self();vm_size_t page=0;host_page_size(host,&page);
 if(host_statistics64(host,HOST_VM_INFO64,(host_info64_t)&vm,&count)==KERN_SUCCESS){
  s.free=(unsigned long long)vm.free_count*page;
  s.available=(unsigned long long)(vm.free_count+vm.inactive_count)*page;
  s.compressed=(unsigned long long)vm.compressor_page_count*page;
 }
 mach_port_deallocate(mach_task_self(),host);
 struct xsw_usage swap={0};n=sizeof(swap);if(sysctlbyname("vm.swapusage",&swap,&n,NULL,0)==0){s.swapTotal=swap.xsu_total;s.swapFree=swap.xsu_avail;}
 CFURLRef url=CFURLCreateFromFileSystemRepresentation(NULL,(const UInt8*)path,strlen(path),true);
 if(url){CFTypeRef value=NULL;
  if(CFURLCopyResourcePropertyForKey(url,kCFURLVolumeNameKey,&value,NULL)&&value){copy_string(value,s.volume,sizeof(s.volume));CFRelease(value);}value=NULL;
  if(CFURLCopyResourcePropertyForKey(url,kCFURLVolumeUUIDStringKey,&value,NULL)&&value){copy_string(value,s.uuid,sizeof(s.uuid));CFRelease(value);}CFRelease(url);
 }
 strcpy(s.power,"Unknown");strcpy(s.charge,"No battery");
 CFTypeRef blob=IOPSCopyPowerSourcesInfo();
 if(blob){copy_string(IOPSGetProvidingPowerSourceType(blob),s.power,sizeof(s.power));
  CFArrayRef list=IOPSCopyPowerSourcesList(blob);
  if(list){for(CFIndex i=0;i<CFArrayGetCount(list);i++){
   CFDictionaryRef d=IOPSGetPowerSourceDescription(blob,CFArrayGetValueAtIndex(list,i));if(!d)continue;
   int capacity=number(d,CFSTR(kIOPSMaxCapacityKey)),current=number(d,CFSTR(kIOPSCurrentCapacityKey));
   if(capacity>0&&current>=0)s.percent=current*100/capacity;
   CFTypeRef charging=CFDictionaryGetValue(d,CFSTR(kIOPSIsChargingKey));
   int isCharging=charging&&CFGetTypeID(charging)==CFBooleanGetTypeID()&&CFBooleanGetValue(charging);
   strcpy(s.charge,isCharging?"Charging":s.percent<0?"Unknown":s.percent<10?"Critical":s.percent<33?"Low":s.percent>66?"High":"Normal");
   s.minutes=number(d,isCharging?CFSTR(kIOPSTimeToFullChargeKey):CFSTR(kIOPSTimeToEmptyKey));break;
  }CFRelease(list);}CFRelease(blob);
 }
 return s;
}
*/
import "C"
import (
	"os"
	"os/user"
	"syscall"
	"unsafe"
)

type Snapshot struct {
	Host, User, Filesystem, Mount, Device, Volume, UUID, Power, Charge string
	Total, Free, Available, Compressed, SwapTotal, SwapFree            uint64
	DiskTotal, DiskFree                                                uint64
	Percent, Minutes                                                   int
}

func Read(path string) Snapshot {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	v := C.collect(cp)
	s := Snapshot{Total: uint64(v.total), Free: uint64(v.free), Available: uint64(v.available), Compressed: uint64(v.compressed), SwapTotal: uint64(v.swapTotal), SwapFree: uint64(v.swapFree), Volume: C.GoString(&v.volume[0]), UUID: C.GoString(&v.uuid[0]), Power: C.GoString(&v.power[0]), Charge: C.GoString(&v.charge[0]), Percent: int(v.percent), Minutes: int(v.minutes)}
	s.Host, _ = os.Hostname()
	if u, err := user.Current(); err == nil {
		s.User = u.Username
	}
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) == nil {
		s.Filesystem = cchars(st.Fstypename[:])
		s.Mount = cchars(st.Mntonname[:])
		s.Device = cchars(st.Mntfromname[:])
		s.DiskTotal = st.Blocks * uint64(st.Bsize)
		s.DiskFree = st.Bavail * uint64(st.Bsize)
	}
	return s
}
func cchars(v []int8) string {
	b := make([]byte, 0, len(v))
	for _, c := range v {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
