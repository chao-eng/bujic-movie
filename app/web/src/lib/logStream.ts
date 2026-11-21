/**
 * 日志流的序号守卫。
 *
 * 服务端给每条日志一个自增 seq，前端靠它做两件事：
 * 1. 断线续传时按 Last-Event-ID 只补缺失的行；
 * 2. 掉线重连可能重发边界那一行，按 seq 去重。
 *
 * 但服务端重启后 seq 会从 1 重新开始。如果前端还记着旧的大序号，
 * 重启后的所有新日志都会被当成「已见过」而丢掉 —— 界面表现为连上了
 * 却一直没有新日志。这里把这段判断抽出来，便于单测。
 */

/** 服务端重启后序号会从 1 开始；小序号一定属于新的一轮。 */
const RESTART_THRESHOLD = 10

export interface SeqOutcome {
  /** 接受这条日志 */
  accept: boolean
  /** 后续应记录的最新序号 */
  nextSeq: number
  /** 是否检测到服务端重启（调用方可以据此提示用户） */
  restarted: boolean
}

/**
 * 收到一条日志时判断是否接受。
 *
 * 只有「本地记着很大的序号、来的却是个很小的序号」才算重启。仅凭 `incoming <
 * local` 判断会把重连时重发的边界行（例如本地 6、重发 6 附近）误当重启，
 * 从而接受重复行，所以加了一道阈值。
 */
export function classifyLogEntry(localSeq: number, incomingSeq: number): SeqOutcome {
  if (incomingSeq <= 0) {
    // 本地提示行（连接中、重连中）没有服务端序号，不推进记录。
    return { accept: false, nextSeq: localSeq, restarted: false }
  }
  if (incomingSeq < localSeq) {
    if (localSeq - incomingSeq >= RESTART_THRESHOLD) {
      // 本地 120、服务端重启后发来 2：新的一轮，接受并重置。
      return { accept: true, nextSeq: incomingSeq, restarted: true }
    }
    // 小幅倒退：重连时重发的旧行，丢弃即可。
    return { accept: false, nextSeq: localSeq, restarted: false }
  }
  if (incomingSeq === localSeq) {
    return { accept: false, nextSeq: localSeq, restarted: false }
  }
  return { accept: true, nextSeq: incomingSeq, restarted: false }
}

/**
 * 收到 ready 事件时，决定是否因服务端重启而重置本地序号。
 *
 * ready 带的是服务端「当前最新序号」。它比本地记录小，或者与本地记录相同
 * 而本地已经收过行，都说明这是新的一轮：新进程刚发出第一条日志时 last_seq
 * 恰好等于旧值的情况很常见（都停在 1），只比对大小会漏掉。
 *
 * 代价是：同一条连接上服务端主动重发 ready 时本地序号会回到该值，但重发
 * 的日志仍会按 classifyLogEntry 去重，不会产生重复行。
 */
export function classifyReady(
  localSeq: number,
  serverLastSeq: number,
  reconnect: boolean,
): { restarted: boolean; nextSeq: number } {
  if (typeof serverLastSeq !== 'number') {
    return { restarted: false, nextSeq: localSeq }
  }
  // 只有「重连后的握手」和「序号大幅倒退」才可能是重启；同一次连接里
  // 正常的 ready 不该清空已收到的内容。
  const wentBack = localSeq - serverLastSeq >= RESTART_THRESHOLD
  const sameSeqAfterReconnect = reconnect && localSeq > 0 && serverLastSeq <= localSeq
  if (wentBack || sameSeqAfterReconnect) {
    // 归零而不是对齐到 serverLastSeq：重启后服务端紧跟着就会推送这些行，
    // 若把 lastSeq 预置成它，随后到达的同号日志会被当成重复丢掉。
    return { restarted: true, nextSeq: 0 }
  }
  return { restarted: false, nextSeq: localSeq }
}
