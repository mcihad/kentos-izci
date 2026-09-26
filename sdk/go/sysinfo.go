package izci

import (
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

var (
	processStart = time.Now()
	mainModule   string
	modules      map[string]string
	modulesOnce  sync.Once
)

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		mainModule = bi.Main.Path
	}
}

// buildModules returns the dependency versions of the binary.
func buildModules() map[string]string {
	modulesOnce.Do(func() {
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		modules = map[string]string{}
		for _, m := range bi.Deps {
			v := m.Version
			if m.Replace != nil {
				v = m.Replace.Version + " (replaced)"
			}
			modules[m.Path] = v
			if len(modules) >= 400 {
				break
			}
		}
	})
	return modules
}

// hostInfo is filled by the platform-specific readSystem.
type hostInfo struct {
	memTotal, memFree, memAvailable uint64
	swapTotal, swapFree             uint64
	loadAvg                         []float64
	osVersion, kernel, distro       string
}

// cpuSampler tracks process CPU usage between two captures.
var cpuSampler struct {
	sync.Mutex
	last    time.Time
	lastCPU time.Duration
}

func processCPUPercent(now time.Time, used time.Duration) *float64 {
	cpuSampler.Lock()
	defer cpuSampler.Unlock()
	defer func() { cpuSampler.last, cpuSampler.lastCPU = now, used }()
	if cpuSampler.last.IsZero() {
		elapsed := now.Sub(processStart)
		if elapsed <= 0 {
			return nil
		}
		p := 100 * used.Seconds() / elapsed.Seconds()
		return &p
	}
	elapsed := now.Sub(cpuSampler.last)
	if elapsed < 10*time.Millisecond {
		return nil
	}
	p := 100 * (used - cpuSampler.lastCPU).Seconds() / elapsed.Seconds()
	return &p
}

// runtimeContexts snapshots runtime, OS, device and process state.
func runtimeContexts() *ingestv1.Contexts {
	now := time.Now()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	host := readSystem()
	userCPU, sysCPU, rss := processUsage()

	c := &ingestv1.Contexts{
		Runtime: &ingestv1.RuntimeContext{Name: "go", Version: strings.TrimPrefix(runtime.Version(), "go"), RawDescription: runtime.Version()},
		Os: &ingestv1.OsContext{Name: osName(), Version: host.osVersion, KernelVersion: host.kernel,
			Distribution: host.distro},
		Device: &ingestv1.DeviceContext{
			Arch:       runtime.GOARCH,
			DeviceType: "server",
			Cpu:        &ingestv1.CpuInfo{Count: uint32(runtime.NumCPU()), LoadAverage: host.loadAvg},
		},
		Process: &ingestv1.ProcessContext{
			Pid:            uint32(os.Getpid()),
			Name:           executableName(),
			StartTime:      timestamppb.New(processStart),
			Uptime:         durationpb.New(now.Sub(processStart).Round(time.Second)),
			RssBytes:       rss,
			HeapAllocBytes: ms.HeapAlloc,
			HeapSysBytes:   ms.HeapSys,
			GcCount:        ms.NumGC,
			GcPauseTotal:   durationpb.New(time.Duration(ms.PauseTotalNs)),
			Goroutines:     uint32(runtime.NumGoroutine()),
			CpuTimeUser:    durationpb.New(userCPU),
			CpuTimeSystem:  durationpb.New(sysCPU),
			CpuPercent:     processCPUPercent(now, userCPU+sysCPU),
		},
	}
	if host.memTotal > 0 {
		mem := &ingestv1.MemoryInfo{TotalBytes: host.memTotal, FreeBytes: host.memFree, AvailableBytes: host.memAvailable,
			SwapTotalBytes: host.swapTotal, SwapFreeBytes: host.swapFree}
		if host.memAvailable > 0 && host.memAvailable <= host.memTotal {
			mem.UsedBytes = host.memTotal - host.memAvailable
			p := 100 * float64(mem.UsedBytes) / float64(host.memTotal)
			mem.UsedPercent = &p
			mem.LowMemory = p > 95
		}
		c.Device.Memory = mem
	}
	if hn, err := os.Hostname(); err == nil {
		c.Device.Name = hn
	}
	if wd, err := os.Getwd(); err == nil {
		c.Process.WorkingDirectory = wd
	}
	return c
}

func executableName() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		return exe[i+1:]
	}
	return exe
}

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	}
	return runtime.GOOS
}
