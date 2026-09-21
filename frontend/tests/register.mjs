// `node --import ./tests/register.mjs` 用：把 ts-resolve.mjs 装进解析链。
import { register } from 'node:module'

register(new URL('./ts-resolve.mjs', import.meta.url), import.meta.url)
