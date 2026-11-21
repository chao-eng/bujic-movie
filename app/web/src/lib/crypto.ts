import { gcm } from '@noble/ciphers/aes'

// Helper to convert hex string to Uint8Array
export function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return bytes
}

// Helper to convert Uint8Array to hex string
export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/**
 * Encrypts a plaintext string using AES-256-GCM.
 * Automatically uses Web Crypto API in secure contexts (HTTPS/localhost),
 * and falls back to @noble/ciphers in non-secure contexts (HTTP).
 * 
 * @param plaintext The string to encrypt (e.g. the password)
 * @param keyHex The 32-byte key in hex format
 * @returns An object containing the hex-encoded ciphertext and iv.
 */
export async function encryptAESGCM(plaintext: string, keyHex: string): Promise<{ ciphertext: string, iv: string }> {
  const keyBytes = hexToBytes(keyHex)
  const plaintextBytes = new TextEncoder().encode(plaintext)
  
  // GCM 的 IV 必须是密码学随机的：IV 复用会让攻击者直接还原明文。
  // 不使用 Math.random() 兜底——它在这个位置不安全，宁可直接报错。
  const ivBytes = crypto.getRandomValues(new Uint8Array(12))

  const isSecure = typeof window !== 'undefined' && window.isSecureContext && Boolean(crypto?.subtle)

  let encryptedBytes: Uint8Array

  if (isSecure) {
    try {
      console.log('[Crypto] Using native Web Crypto API')
      const cryptoKey = await crypto.subtle.importKey(
        'raw',
        keyBytes as any,
        { name: 'AES-GCM' },
        false,
        ['encrypt']
      )
      const encryptedBuffer = await crypto.subtle.encrypt(
        {
          name: 'AES-GCM',
          iv: ivBytes as any,
        },
        cryptoKey,
        plaintextBytes as any
      )
      encryptedBytes = new Uint8Array(encryptedBuffer)
    } catch (err) {
      console.warn('[Crypto] Native Web Crypto failed, falling back to JS implementation:', err)
      // Fallback inside catch
      encryptedBytes = gcm(keyBytes, ivBytes).encrypt(plaintextBytes)
    }
  } else {
    console.log('[Crypto] Non-secure context, using @noble/ciphers fallback')
    encryptedBytes = gcm(keyBytes, ivBytes).encrypt(plaintextBytes)
  }

  return {
    ciphertext: bytesToHex(encryptedBytes),
    iv: bytesToHex(ivBytes),
  }
}
