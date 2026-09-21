// Node 原生 ESM 要求相对导入带完整文件名，而 src/ 走的是 tsconfig 的 bundler 解析
// （`from './format'` 这种省略扩展名写法）。为了让测试能直接 import src 里的模块，
// 又不把 src 的导入风格改成只配合 Node（改一处就得全仓跟进，且 Vite 并不需要），
// 解析失败时在这里补试 `.ts` / `/index.ts` 两种形态。
export async function resolve(specifier, context, next) {
  try {
    return await next(specifier, context)
  } catch (err) {
    const isRelative = specifier.startsWith('./') || specifier.startsWith('../')
    if (err?.code !== 'ERR_MODULE_NOT_FOUND' || !isRelative || !context.parentURL) throw err
    const base = new URL(specifier, context.parentURL).href
    for (const suffix of ['.ts', '/index.ts']) {
      try {
        return await next(base + suffix, context)
      } catch {
        /* 换下一种形态再试 */
      }
    }
    throw err
  }
}
