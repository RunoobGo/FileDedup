// P2-3：模态浮层的通用无障碍行为。
//
// 修复前三个浮层（失败抽屉 / 确认对话框 / 预览面板）都只有视觉遮罩：
// 容器上 role / aria-modal / aria-label 全为 null，打开后 document.activeElement 仍是 BODY。
// 后果是键盘用户按 Tab 会一路走进被遮罩挡住的组列表里，关闭浮层后焦点也回不到原处。
//
// 这里统一处理三件事，三个浮层共用同一份实现，避免各写一套：
//   1) 打开时把焦点移入浮层（首个可聚焦元素；一个都没有时聚焦浮层容器本身）；
//   2) Tab / Shift+Tab 循环限制在浮层内，焦点不逃逸到背景内容；
//   3) 关闭时把焦点归还原先的触发元素。
//
// 用法：
//   const dlgRef = ref<HTMLElement | null>(null)
//   useModal(dlgRef, () => store.failedOpen)      // 组件常驻、靠 flag 开关时必须传第二个参数
//   useModal(dlgRef)                              // 组件本身由 v-if 控制（打开时才挂载）可省略
// 模板上给浮层容器加 ref / role="dialog" / aria-modal="true" / tabindex="-1"。
//
// 注意第二个参数不是可有可无的装饰：FailedDrawer / PreviewPanel 由 App.vue 常驻挂载、
// 内部才用 v-if 控制显隐，若只依赖 onMounted 会在应用启动时（尚未打开）就执行一次，
// 之后再也拿不到焦点。必须监听「打开」这个状态变化。
import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

const FOCUSABLE = [
  'a[href]',
  'button:not(:disabled)',
  'input:not(:disabled)',
  'select:not(:disabled)',
  'textarea:not(:disabled)',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export function useModal(rootRef: Ref<HTMLElement | null>, isOpen: () => boolean = () => true) {
  let restoreTo: HTMLElement | null = null
  let rootEl: HTMLElement | null = null
  let active = false
  // 每次 activate/deactivate 都递增；异步等待后 token 不一致说明期间已关闭或重开，直接放弃
  let token = 0

  function focusables(): HTMLElement[] {
    if (!rootEl) return []
    return Array.from(rootEl.querySelectorAll<HTMLElement>(FOCUSABLE))
      // offsetParent 为 null 说明不可见（例如按 kind 条件渲染的行）
      .filter((el) => el.offsetParent !== null)
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Tab' || !active || !rootEl) return
    const list = focusables()
    if (list.length === 0) {
      e.preventDefault()
      rootEl.focus()
      return
    }
    const first = list[0]
    const last = list[list.length - 1]
    const cur = document.activeElement as HTMLElement | null
    // 焦点不在浮层内（含掉到 BODY）时拉回来
    if (!cur || !rootEl.contains(cur)) {
      e.preventDefault()
      ;(e.shiftKey ? last : first).focus()
      return
    }
    if (e.shiftKey && cur === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && cur === last) {
      e.preventDefault()
      first.focus()
    }
  }

  async function activate() {
    const my = ++token
    await nextTick() // 等 v-if 把浮层渲染出来
    if (my !== token) return
    rootEl = rootRef.value
    if (!rootEl) return
    active = true
    restoreTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
    document.addEventListener('keydown', onKeydown, true)
    if (!rootEl.hasAttribute('tabindex')) rootEl.setAttribute('tabindex', '-1')
    const list = focusables()
    if (list.length) list[0].focus()
    else rootEl.focus()
  }

  function deactivate() {
    token++
    if (!active) return
    document.removeEventListener('keydown', onKeydown, true)
    const cur = document.activeElement as HTMLElement | null
    // 只有焦点还在浮层内、或已掉到 body 时才归还；
    // 用户若已主动把焦点移到别处（如快捷键切了页面），就不抢回来。
    const shouldRestore =
      !cur || cur === document.body || (rootEl ? rootEl.contains(cur) : false)
    if (shouldRestore) restoreTo?.focus?.()
    active = false
    rootEl = null
  }

  watch(
    isOpen,
    (v) => {
      if (v) void activate()
      else deactivate()
    },
    { immediate: true, flush: 'post' },
  )

  onBeforeUnmount(deactivate)
}
