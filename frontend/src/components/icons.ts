// P2-1：统一线性图标集的几何数据。
//
// 独立成 .ts 而非放在 Icon.vue 的 <script setup> 里，原因有二：
//   1. <script setup> 不允许 ES 模块导出，把类型放在那里依赖编译器的宽容行为；
//   2. 图标名是跨组件的公共契约（导航、按钮、角标都要引用），放在纯 TS 模块里
//      才能被 tsc 正常检查，也能被非组件代码引用。
//
// 几何取自 Lucide 的 24×24 线性图标（MIT），保证线宽、端点、圆角语言一致。

export type IconName =
  | 'scan' // 扫描（导航）
  | 'layers' // 结果（导航）—— 重复组 = 叠层副本
  | 'gear' // 设置（导航）
  | 'eye' // 预览
  | 'folder-open' // 打开所在文件夹
  | 'star' // 保留
  | 'close' // 冗余 / 关闭 / 移除
  | 'check' // 成功 / 已保存
  | 'check-circle' // 空态（无重复）
  | 'alert' // 失败 / 错误 / 警告
  | 'info' // 提示
  | 'skip' // 已跳过
  | 'chevron-down' // 折叠指示
  | 'history' // 记录（导航）—— 扫描历史 / 清理记录
  | 'undo' // 回撤（清理记录）
  | 'filter' // 处理范围已收窄（处理策略的过滤提示）—— 漏斗，语义直白

export interface IconDef {
  /** 一段或多段 path 的 d 属性 */
  d: string[]
  /** 可选的圆（画在 path 之后） */
  circle?: [number, number, number]
  /** 图标集网格边长，用于定位几何来源；导入的路径均按此网格绘制 */
  viewBox: number
}

export const ICON_SIZE = 24

export const ICONS: Record<IconName, IconDef> = {
  scan: {
    viewBox: ICON_SIZE,
    d: [
      'M3 7V5a2 2 0 0 1 2-2h2',
      'M17 3h2a2 2 0 0 1 2 2v2',
      'M21 17v2a2 2 0 0 1-2 2h-2',
      'M7 21H5a2 2 0 0 1-2-2v-2',
      'M3 12h18',
    ],
  },
  layers: {
    viewBox: ICON_SIZE,
    d: [
      'M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z',
      'm22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65',
      'm22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65',
    ],
  },
  gear: {
    viewBox: ICON_SIZE,
    d: [
      'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z',
    ],
    circle: [12, 12, 3],
  },
  eye: {
    viewBox: ICON_SIZE,
    d: ['M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0'],
    circle: [12, 12, 3],
  },
  'folder-open': {
    viewBox: ICON_SIZE,
    d: [
      'm6 14 1.45-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.55 6a2 2 0 0 1-1.94 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H18a2 2 0 0 1 2 2v2',
    ],
  },
  star: {
    viewBox: ICON_SIZE,
    d: [
      'M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z',
    ],
  },
  close: { viewBox: ICON_SIZE, d: ['M18 6 6 18', 'm6 6 12 12'] },
  check: { viewBox: ICON_SIZE, d: ['M20 6 9 17l-5-5'] },
  'check-circle': {
    viewBox: ICON_SIZE,
    d: ['M21.801 10A10 10 0 1 1 17 3.335', 'm9 11 3 3L22 4'],
  },
  alert: {
    viewBox: ICON_SIZE,
    d: ['m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3', 'M12 9v4', 'M12 17h.01'],
  },
  info: { viewBox: ICON_SIZE, d: ['M12 16v-4', 'M12 8h.01'], circle: [12, 12, 10] },
  skip: { viewBox: ICON_SIZE, d: ['m15 14 5-5-5-5', 'M4 20v-7a4 4 0 0 1 4-4h12'] },
  'chevron-down': { viewBox: ICON_SIZE, d: ['m6 9 6 6 6-6'] },
  // lucide clock：圆 + 指针（扫描/清理历史）
  history: {
    viewBox: ICON_SIZE,
    d: ['M12 6v6l4 2'],
    circle: [12, 12, 10],
  },
  // lucide rotate-ccw：回撤
  undo: {
    viewBox: ICON_SIZE,
    d: ['M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8', 'M3 3v5h5'],
  },
  filter: {
    viewBox: ICON_SIZE,
    // 漏斗（Lucide filter）：上宽下窄，表达"从全集里筛出子集"。
    d: ['M22 3H2l8 9.46V19l4 2v-8.54L22 3z'],
  },
}

/** 角标/状态语义到图标的固定映射，避免各处自行挑图标导致语义漂移 */
export const KIND_ICONS = {
  error: 'close',
  warn: 'alert',
  info: 'info',
  success: 'check',
} as const satisfies Record<string, IconName>
