// Package ads 判定一个文件是否挂有 NTFS 备用数据流（ADS），用于在**动手删/移/链之前**
// 拦下"去重会连备用流一起丢掉"的操作（M6-P3，04 §6.7 C 组 3 / 总纲 §2.3）。
//
// 为什么需要它：本产品的全部判据只看**默认数据流**（扫描按逻辑大小、哈希按默认流内容），
// 于是 NTFS 上两个"默认流逐字节相同、命名流完全不同"的文件会被判成重复。把其中一份
// 移走/删除/替换成链接，它自己那些命名流就**永久消失**——这不是"少省一点空间"，
// 是静默丢用户数据。所以本项不试图把备用流算进身份（那是 M35），只在动手前拦一道。
//
// 分层约定（与 internal/sysguard、internal/realbytes、internal/cloudfile 同族）：
//
//   - 本文件无 build tag，承载**全部判定规则**：流名判据、errno 分类、fail-closed 表。
//     这三样都是安全决策而非平台细节，放在这里就能在 Linux 主门禁里逐格断言；
//   - 带 tag 的文件只做系统调用胶水，把"名字列表 + 最后一个 errno"交上来，不判任何事。
package ads

import (
	"fmt"
	"strings"
)

// 需要区分的五个 Win32 错误码。真值出处（MSDN 原文见设计稿 §5.0 E4/E8）：
//
//	ERROR_HANDLE_EOF         38   没有更多流可枚举（E8：FindNextStreamW 的正常终止）
//	ERROR_INVALID_PARAMETER  87   "该文件系统不支持流"（E4 原文，Return value 段）
//	ERROR_FILE_NOT_FOUND      2
//	ERROR_PATH_NOT_FOUND      3
//	ERROR_ACCESS_DENIED       5
//
// 与 internal/ops/crossdevice_windows.go:10-23 抄录 Win32 错误码同一手法：本仓不引
// x/sys，本地定义并附出处。
const (
	errHandleEOF        = 38
	errInvalidParameter = 87
	errFileNotFound     = 2
	errPathNotFound     = 3
	errAccessDenied     = 5
)

// ErrKind 是一次流枚举的**结局种类**。它的存在意义是让"哪些结局可以放行"成为一张
// 可以在 Linux 上断言的表（Decide），而不是散在 windows 文件里的 if。
type ErrKind int

const (
	// ErrNone：枚举完成，names 可信。
	ErrNone ErrKind = iota
	// ErrNoStreams：ERROR_HANDLE_EOF，一个流都没有（含目录与竞态）。
	ErrNoStreams
	// ErrFSNoStreams：该文件系统不支持备用流（exFAT/FAT U 盘、部分网络盘）。
	ErrFSNoStreams
	// ErrMissing：文件/路径已不存在。
	ErrMissing
	// ErrDenied：拒绝访问——判不了。
	ErrDenied
	// ErrOther：其余一切——判不了。
	ErrOther
)

func (k ErrKind) String() string {
	switch k {
	case ErrNone:
		return "枚举成功"
	case ErrNoStreams:
		return "没有流"
	case ErrFSNoStreams:
		return "该卷不支持备用数据流"
	case ErrMissing:
		return "文件已不存在"
	case ErrDenied:
		return "拒绝访问"
	default:
		return "未知错误"
	}
}

// Classify 把 Win32 errno 折成 ErrKind。分类表故意做成**穷尽**的：没有 default 之外的
// 分片，任何不认识的码都落 ErrOther → Reject（未知即不赌）。
func Classify(errno uintptr) ErrKind {
	switch errno {
	case 0:
		return ErrNone
	case errHandleEOF:
		return ErrNoStreams
	case errInvalidParameter:
		return ErrFSNoStreams
	case errFileNotFound, errPathNotFound:
		return ErrMissing
	case errAccessDenied:
		return ErrDenied
	default:
		return ErrOther
	}
}

// StreamIsNonDefault 判定一个由 FindFirstStreamW 回吐的流名**不是**那个默认的无名
// 数据流。流名的字符串格式是 ":streamname:$streamtype"（E6）。
//
// 形状（2026-09-21 实施时相对设计稿 §5.1 的一处收紧，理由见 V1 用例表）：
//
//  1. 没有前导冒号 → 不是流记法，**不判**。这一档收的是畸形串与平台差异，
//     判成"有备用流"会让整卷每个文件都被拒；判成"没有"最坏是漏一个本来也
//     枚举不出来的东西。
//  2. 有前导冒号 → 取**最后一个**冒号做名字段/型别段的分界，名字段非空即是命名流。
//     流名本身不允许含冒号，用最后一段分界可让 ":Zone.Identifier::$DATA" 这类
//     多冒号的畸形串落进"命名流"一侧——宁可多拦一个，也不漏判一个。
//  3. 没有第二个冒号时只有两种可能："::$DATA" 这种空名字段形态（E5 的规范写法，
//     放行）与 ":note" 这种省略型别段的命名流（拦住）。靠大小写不敏感地比 $DATA
//     区分——这一格就是 M-P3-c 变异打的地方。
//
// 误判默认流的代价是**整卷每个文件都被拒**，所以三种默认写法（"::$DATA"、
// "::$data"、":$DATA"）在用例里是并列的必过项。
func StreamIsNonDefault(name string) bool {
	rest, ok := strings.CutPrefix(name, ":")
	if !ok || rest == "" {
		return false
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		return i > 0
	}
	return !strings.EqualFold(rest, "$DATA")
}

// HasNonDefault 报告一组流名里有没有命名流。
func HasNonDefault(names []string) bool {
	for _, n := range names {
		if StreamIsNonDefault(n) {
			return true
		}
	}
	return false
}

// Outcome 是一次判定的结果。Reject 为真时必须带 Reason（会原样进失败抽屉，
// 用户据此决定"手工处理"还是"重扫"）；放行时 Reason 恒为空串。
type Outcome struct {
	Reject bool
	Reason string
}

const (
	reasonNamed = "文件含备用数据流，去重会丢失备用流内容，已拒绝操作"
	// 判不了就拒：与 internal/ops/verify.go:104-108 的同族决策同向——
	// "宁可拦一次让用户重扫，也不放行一次可能覆盖他人文件的操作"。
	reasonUnknownFmt = "无法确认文件是否存在备用数据流（%s），为避免丢失其内容已拒绝操作"
)

// Decide 是 fail-closed 表本身（设计 §5.1）。哪些结局放行是**安全决策**，
// 所以它在这里而不是在 windows 文件里——否则这张表只有 Windows runner 能测，
// 而本仓没有 runner（M32）。
//
//	ErrNone          按 names 判
//	ErrNoStreams     放行：一个 $DATA 都没有，无从谈起备用流
//	ErrFSNoStreams   放行：卷不支持流，判拒绝等于让这些卷上每次清理都被拒
//	ErrMissing       放行：文件已消失，属"目标已达成"的邻居，紧接着的
//	                 MoveFile/Trash 会给出自己的真实错误——报成"有备用流"就是说谎
//	ErrDenied        拒绝：判不了
//	ErrOther         拒绝：未知即不赌。丢数据不可回撤，多拦一次可重扫
func Decide(names []string, k ErrKind) Outcome {
	switch k {
	case ErrNone:
		if HasNonDefault(names) {
			return Outcome{Reject: true, Reason: reasonNamed}
		}
		return Outcome{}
	case ErrNoStreams, ErrFSNoStreams, ErrMissing:
		return Outcome{}
	default: // ErrDenied / ErrOther
		return Outcome{Reject: true, Reason: fmt.Sprintf(reasonUnknownFmt, k)}
	}
}

// Check 是执行器侧的入口：枚举一次，按上表给结论。
// 注意它**不**区分平台——非 Windows 上 enumerateStreams 恒给 (nil, 0)，
// 自然落到 ErrNone → 放行（本平台无 ADS 语义，不猜）。
func Check(path string) Outcome {
	names, errno := enumerateStreams(path)
	return Decide(names, Classify(errno))
}
