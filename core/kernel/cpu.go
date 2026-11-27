package kernel

import "golang.org/x/sys/cpu"

// HostX86V3 reports whether this CPU runs x86-64-v3 code (GOAMD64=v3, what
// mihomo's plain amd64 assets are built for): the v2 set plus AVX, AVX2,
// BMI1, BMI2, F16C, FMA, LZCNT, MOVBE and OS support for the AVX state
// (x/sys/cpu's HasAVX already requires it). Always false off amd64.
func HostX86V3() bool {
	x := cpu.X86
	return x.HasCX16 && x.HasLAHF && x.HasPOPCNT && x.HasSSE3 && x.HasSSSE3 && x.HasSSE41 && x.HasSSE42 &&
		x.HasAVX && x.HasAVX2 && x.HasBMI1 && x.HasBMI2 && x.HasF16C && x.HasFMA && x.HasLZCNT && x.HasMOVBE && x.HasOSXSAVE
}
