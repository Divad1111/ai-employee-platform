export type SavedIndex = { reindexed?: number; index_warning?: string }

// Index failure must not turn a committed save into a retryable save failure.
export async function rebuildSavedIndex<T extends object>(saved: T, rebuild: () => Promise<{ reindexed: number }>): Promise<T & SavedIndex> {
  try {
    const result = await rebuild()
    return { ...saved, reindexed: result.reindexed }
  } catch (error) {
    return { ...saved, index_warning: error instanceof Error ? error.message : String(error) }
  }
}

export function savedIndexMessage(message: string, result: SavedIndex): string {
  if (result.index_warning) return `${message}；自动重建知识索引失败：${result.index_warning}。可点击“重建知识索引”重试，无需重复保存。`
  if (result.reindexed !== undefined) return `${message}；已自动重建 ${result.reindexed} 篇知识文档索引`
  return message
}
