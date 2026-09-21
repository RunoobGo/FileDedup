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
    /** 同步派发一个 Wails 事件（真实运行时也是从后端 goroutine 异步推给 JS 的）。 */
    emit(name, data) {
      for (const cb of handlers.get(name) ?? []) cb(data)
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
