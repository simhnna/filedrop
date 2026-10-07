import { describe, expect, it } from 'vitest'
import { generateRoomName, normalizeRoomCode, parseRoomId, ROOM_WORDS } from './roomName'
import { WORDLIST } from './wordlist'
import vectors from '../../testdata/compat.json'
import goWordlist from '../../cli/internal/roomname/wordlist.go?raw'

describe('compatibility with the CLI', () => {
  it('uses the same wordlist', () => {
    const goWords = [...goWordlist.matchAll(/"([^"]*)"/g)].map(m => m[1])
    expect(goWords).toEqual([...WORDLIST])
  })

  for (const c of vectors.roomCodes) {
    it(`normalizes ${JSON.stringify(c.input)}`, () => {
      const result = normalizeRoomCode(c.input)
      if (c.roomId === null) expect(result).toHaveProperty('error')
      else expect(result).toEqual({ roomId: c.roomId })
    })
  }

  for (const c of vectors.splitRoom) {
    it(`splits ${c.roomId}`, () => {
      expect(parseRoomId(c.roomId)).toEqual({ signalingId: c.topic, secret: c.secret })
    })
  }
})

describe('wordlist', () => {
  it('has no duplicates and only normalizable words', () => {
    expect(new Set(WORDLIST).size).toBe(WORDLIST.length)
    for (const w of WORDLIST) expect(w).toMatch(/^[a-z]+$/)
  })
})

describe('generateRoomName', () => {
  it('generates canonical codes', () => {
    for (let i = 0; i < 100; i++) {
      const name = generateRoomName()
      expect(name.split('-')).toHaveLength(ROOM_WORDS)
      expect(normalizeRoomCode(name)).toEqual({ roomId: name })
    }
  })
})
