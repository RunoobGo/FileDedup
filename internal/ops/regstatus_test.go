package ops

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

// TestClassifyRegErrMapsTypeMismatchToUnknown AS-R5：类型不符此前有专用
// 信号（errRegTypeMismatch / regIsMismatch）却无人引用，与 ACL 拒绝一起走
// 泛化 fall-through。分错的代价是**方向性的**：若把它并进 not-found 那一支，
// 「用户手工写了个字符串值」就被读成「用户没设过策略」→ nukeOff → 放行 →
// 整批文件永久删除。本用例把这条边界钉成平台无关的显式契约。
func TestClassifyRegErrMapsTypeMismatchToUnknown(t *testing.T) {
	wrapped := fmt.Errorf("读取失败: %w", errRegTypeMismatch)
	if got := classifyRegErr(wrapped); got != nukeUnknown {
		t.Fatalf("类型不符必须判 nukeUnknown（未知≠未禁用），got %v", got)
	}
	if got := classifyRegErr(errRegTypeMismatch); got != nukeUnknown {
		t.Fatalf("未包装的类型不符同样必须判 nukeUnknown，got %v", got)
	}
	// 负例：不得因为"不是 ACL"就掉进 not-found 分支
	if classifyRegErr(wrapped) == nukeOff {
		t.Fatal("类型不符被归入 nukeOff = fail-open，永久删除策略会被读成未启用")
	}
}

func TestClassifyRegErrMapsNotFoundToOff(t *testing.T) {
	for _, err := range []error{
		syscall.Errno(regErrNotFound),
		syscall.Errno(3), // ERROR_PATH_NOT_FOUND：父键不存在，同属"没有这个设置"
		fmt.Errorf("open %w: BitBucket", syscall.Errno(2)),
	} {
		if got := classifyRegErr(err); got != nukeOff {
			t.Fatalf("%v 是「没设过」的正常情形，应判 nukeOff，got %v", err, got)
		}
	}
	// 负例：ACL 拒绝（ERROR_ACCESS_DENIED=5）不是"没设过"，不得判 nukeOff
	if got := classifyRegErr(syscall.Errno(5)); got != nukeUnknown {
		t.Fatalf("权限拒绝应判 nukeUnknown（交事后复核），got %v", got)
	}
	// 负例：随机错误不得被判成 nukeOff
	if got := classifyRegErr(errors.New("unexpected")); got != nukeUnknown {
		t.Fatalf("未归类错误必须保守判 nukeUnknown，got %v", got)
	}
}
