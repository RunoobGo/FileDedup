// 前端 store 行为探针的桩具（M16 起）。
//
// 为什么需要它：scan.ts 的状态机缺陷（事件与 RPC 回包竞速）只有"真的跑一遍状态机"
// 才证得出来，vue-tsc 与 vite build 都只会证明它能编译。仓库没有 JS 测试运行器，
// 但 pinia/vue 本来就在 frontend/node_modules 里（它们是运行时依赖，不是新增依赖），
// Node 24 又自带 TS 类型剥离，于是可以直接在 Node 里实例化真 store。
//
// 桩的面：wails.ts 只认 window.go.main.App / window.runtime.EventsOn，
// 且全部访问都被 `typeof window === 'undefined'` 保护，所以给它一个 Proxy 就够。
// 刻意用 Proxy：BackendAPI 有 30+ 个方法，逐个手写桩会把测试写成接口的复刻品，
// 接口一改就漏；Proxy 只记录"被调了什么"，未登记的调用返回 null（与后端空回包同形）。
//
// ★ 这枚桩的盲区（M304，2026-09-27 现读记档）：
//
// (1) `has: () => true` 声明"任何属性都存在"。**今天它不被触发**：
//     `backendOrNull()`（wails.ts:433-438）走的是可选链 + 真值判断
//     （`window.go?.main` → `main.App ?? main.app?.App`），全仓 src 里没有任何
//     `x in window.go.main.App` 形状的检查（现读 grep 为空）。
//     所以把它改成"按真实方法集回答成员与否"在当前代码上是**零行为差**的——
//     真要收口这一面，得先有消费 `in` 的检查面，否则改的是装饰。
//
// (2) 真正活着的是 `get` 那一面：任意方法名都返回一个可用桩，于是
//     "前端调了一枚 Go 侧没有的方法"在 store 测试里**不红**。
//     这一面不在这里补，因为它已有两处更上游、且比桩具接近真值的钉子：
//       · `backend()` 的返回类型是 `BackendAPI` ⇒ 调未声明方法是编译错误（vue-tsc 门禁在管）；
//       · `TestBackendAPIMatchesGoExportedMethods`（M46/G5，根包）把 `BackendAPI` 声明 ↔
//         Go 反射导出方法集钉成**双向零豁免**。
//     ★ 事件名那一面**没有**这种类型层可依托（Go 与 TS 两边都是裸字符串），
//       所以它由 `frontend/tests/event-channel-names.test.ts`（M303）在源码层比双向集合，
//       与桩具无关、不经这里。
//
// (3) 本批已收口的一处：`emit(name)` 过去对"没人订阅的名字"**静默空转**——
//     循环体一次都不执行，既不报错也不计数，于是"发了事件、断言了状态、
//     但那格状态其实是别的路径写的"能读成绿。现在默认抛，只有显式传
//     `allowNoListener` 并断言返回 0 的用例（M196-C 那一格"解绑后不再收"）
//     才允许走到空订阅者分支——把"桩具碰巧不吭声"变成"测试自己说清楚了"。
export function installWailsStub(responders = {}) {
  const handlers = new Map()
  const calls = []

  const app = new Proxy(
    {},
    {
      get(_target, prop) {
        if (typeof prop !== 'string') return undefined
        return (...args) => {
          calls.push({ method: prop, args })
          const r = responders[prop]
          if (r === undefined) return Promise.resolve(null)
          return Promise.resolve(typeof r === 'function' ? r(...args) : r)
        }
      },
      has: () => true,
    },
  )

  globalThis.window = {
    go: { main: { App: app } },
    runtime: {
      EventsOn: (name, cb) => {
        const list = handlers.get(name) ?? []
        list.push(cb)
        handlers.set(name, list)
      },
      EventsOff: (name) => {
        handlers.delete(name)
      },
    },
    // applyTheme 在 theme==='system' 时才读 matchMedia；给个固定值免得依赖环境。
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    addEventListener() {},
    removeEventListener() {},
  }
  globalThis.document = {
    documentElement: { dataset: {} },
    addEventListener() {},
    removeEventListener() {},
  }

  return {
    calls,
    /** 某后端方法被调了几次（用来断言"重放只发生在真需要的分支"）。 */
    invoked: (method) => calls.filter((c) => c.method === method).length,
    /**
     * 同步派发一个 Wails 事件（真实运行时也是从后端 goroutine 异步推给 JS 的）。
     * 返回接住的回调数。**没有订阅者时默认抛**（M304）：静默空转会把
     * "发了事件、断言了状态、但那格状态其实是别的路径写的"读成绿。
     * 刻意测"解绑后不再收"的用例传 `allowNoListener`，并把返回值钉成 0——
     * 让这一格从"桩具碰巧不吭声"变成"测试自己说清楚了"。
     */
    emit(name, data, { allowNoListener = false } = {}) {
      const list = handlers.get(name) ?? []
      if (list.length === 0 && !allowNoListener) {
        throw new Error(
          `桩具 emit：没有任何订阅者接住 "${name}"（事件名拼错 / 生产侧已改名 / 忘了 bindEvents）` +
            '——空转一次等于没断言；若这是刻意的"解绑后不再收"，请传 allowNoListener 并断言返回 0',
        )
      }
      for (const cb of list) cb(data)
      return list.length
    },
    restore() {
      delete globalThis.window
      delete globalThis.document
    },
  }
}

/** 让 Promise 链跑到底：store 里有多层 await + 串行链，单靠一次 await 不够。 */
export async function flush(turns = 50) {
  for (let i = 0; i < turns; i++) await new Promise((r) => setImmediate(r))
}

/** 可控 resolve 的 Promise，用于把 RPC 回包停在事件之后。 */
export function deferred() {
  let resolve = () => {}
  let reject = () => {}
  const promise = new Promise((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}
