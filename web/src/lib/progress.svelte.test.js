import { describe, it, expect } from 'vitest'
import { getProgress, markJobActiveOptimistically, handleProgressMessage } from './progress.svelte.js'

// The store keeps module-level $state; tests share one instance, so each case
// drives the store into the precondition it needs before asserting.
describe('markJobActiveOptimistically', () => {
  const cases = [
    {
      name: 'marks the clicked job active with running=true and no run_id',
      pre: () => {},
      act: () => markJobActiveOptimistically(5, 'plex'),
      assert: (p) => p.running === true && p.activeRun?.job_id === 5 && p.activeRun?.run_id === null && p.activeRun?.job_name === 'plex',
    },
    {
      name: 'defaults the job name to "Job #<id>" when not provided',
      pre: () => { handleProgressMessage({ type: 'job_run_completed', job_id: 5, run_id: null }) },
      act: () => markJobActiveOptimistically(7),
      assert: (p) => p.activeRun?.job_name === 'Job #7',
    },
    {
      name: 'does not clobber an already-live run',
      pre: () => {
        handleProgressMessage({ type: 'job_run_completed', job_id: 5, run_id: null })
        markJobActiveOptimistically(5, 'plex')
      },
      act: () => markJobActiveOptimistically(9, 'other'),
      assert: (p) => p.activeRun?.job_id === 5,
    },
    {
      name: 'a real job_run_started event replaces the optimistic placeholder',
      pre: () => { markJobActiveOptimistically(5, 'plex') },
      act: () => handleProgressMessage({ type: 'job_run_started', job_id: 5, run_id: 42, job_name: 'plex', items_total: 3 }),
      assert: (p) => p.activeRun?.run_id === 42 && p.activeRun?.job_id === 5 && p.overallTotal === 3,
    },
    {
      name: 'job_run_completed clears running even after an optimistic start',
      pre: () => { markJobActiveOptimistically(5, 'plex'); handleProgressMessage({ type: 'job_run_started', job_id: 5, run_id: 42, job_name: 'plex' }) },
      act: () => handleProgressMessage({ type: 'job_run_completed', job_id: 5, run_id: 42 }),
      assert: (p) => p.running === false,
    },
  ]

  for (const c of cases) {
    it(c.name, () => {
      c.pre()
      c.act(getProgress())
      expect(c.assert(getProgress())).toBe(true)
    })
  }
})
