"""Behavior and failure probes for Sama's local-store adapter."""
import concurrent.futures
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
import collab as c

CONVERSATION = '''# Record
## Request
A synthetic local request.
> Please check this fixture.
## Interpretation
Local tooling fixture only.
## Actions and changes
No application change.
## Verification
### Verified
The fixture was inspected locally.
### Not run
No cloud resources.
## Decisions and rule candidates
None.
## Uncertainty
No production claims.
## Handoff
### Done
Fixture complete.
### Next step
None.
### Blockers
None.
### Reading order
Project rules.
'''
RESEARCH = '''# Research
## Epistemic Boundary
This toy example is illustrative, not model telemetry or private reasoning.
## Observable Context
A local synthetic fixture is supplied to the agent.
## AI Concept
External validation of generated structured text.
## Worked Example
Suppose a toy generator writes two objects with the same identity. A set-based checker inserts the first identity and rejects the second, independently of any confidence assigned by the generator. This is a checkable algorithm, not a probability claim about a real model.
## Connection to This Exchange
The synthetic duplicate can be verified by the test runner.
## Limits
No actual hidden activations were measured.
## Sources
This explicitly synthetic test fixture and the local schema.
## Learning Experiment
Duplicate an event and observe the validator reject it.
'''


class StoreTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name).resolve()
        subprocess.run(['git','init','-q',str(self.repo)],check=True)
        (self.repo/'.gitignore').write_text('/agents/\n')

    def create(self, **kwargs):
        return c.new(self.repo, 'test-record', 'Test Agent', 'analyst',
                     kwargs.get('conversation'), kwargs.get('text','Please check this fixture.'),
                     kwargs.get('supersedes'))

    def fill(self, e):
        for kind in ('conversation','research','commands'):
            p=self.repo/'agents'/e['files'][kind]
            meta,_=c.engine.parse_document(p.read_text())
            if kind=='research': meta['concept']='structured-generation-validation'
            body={'conversation':CONVERSATION,'research':RESEARCH,
                  'commands':'# Command inventory\n\nNo commands were run.\n'}[kind]
            p.write_text(c.engine.render_document(meta,body))

    def final(self):
        e=self.create(); self.fill(e)
        return c.finalize(self.repo,e['event_id'],'working-tree')

    def valid(self, drafts=False):
        return c.validate(self.repo,c.load(self.repo),allow_drafts=drafts)

    def mutate_doc(self, fn):
        doc=c.load(self.repo); fn(doc); c.store(self.repo,doc)

    def test_fresh_clone_lint_is_read_only(self):
        self.assertEqual(c.main(['lint',str(self.repo)]),0)
        self.assertFalse((self.repo/'agents').exists())

    def test_full_lifecycle_and_correction(self):
        e=self.final(); self.assertEqual(self.valid(),1)
        nxt=self.create(conversation=e['conversation_id'],supersedes=e['event_id'])
        self.assertEqual(nxt['sequence'],2)
        self.assertEqual(len(set(Path(p).name for p in nxt['files'].values())),1)
        self.fill(nxt); c.finalize(self.repo,nxt['event_id'],'staged')
        self.assertEqual(self.valid(),2)

    def test_draft_and_placeholders_block(self):
        e=self.create()
        with self.assertRaisesRegex(c.Error,'unfinished draft'): self.valid()
        with self.assertRaisesRegex(c.Error,'placeholder'): c.finalize(self.repo,e['event_id'],'working-tree')

    def test_final_file_tampering_blocks(self):
        e=self.final(); p=self.repo/'agents'/e['files']['input']
        p.write_text(p.read_text()+'Changed.\n')
        with self.assertRaisesRegex(c.Error,'digest mismatch'): self.valid()

    def test_final_history_metadata_tampering_blocks(self):
        self.final(); self.mutate_doc(lambda d:d['events'][0].update(agent='Different Agent'))
        with self.assertRaisesRegex(c.Error,'metadata digest'): self.valid()

    def test_duplicate_and_sequence_gap(self):
        self.final(); self.mutate_doc(lambda d:d['events'].append(dict(d['events'][0])))
        with self.assertRaisesRegex(c.Error,'duplicate event'): self.valid()

    def test_sequence_gap_rejected(self):
        self.create(); self.mutate_doc(lambda d:d['events'][0].update(sequence=3))
        with self.assertRaisesRegex(c.Error,'sequence gap'): self.valid(drafts=True)

    def test_research_metadata_placeholder_rejected(self):
        e=self.create(); self.fill(e); p=self.repo/'agents'/e['files']['research']
        p.write_text(p.read_text().replace('structured-generation-validation','REPLACE_AI_CONCEPT'))
        with self.assertRaisesRegex(c.Error,'metadata has unfinished'): c.finalize(self.repo,e['event_id'],'working-tree')

    def test_upstream_entrypoint_disabled(self):
        result=subprocess.run([sys.executable,str(SCRIPTS/'agent_collaboration/upstream.py'),'build',str(self.repo)],capture_output=True,text=True,timeout=10)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('Use scripts/collab.py',result.stderr)
        self.assertFalse((self.repo/'.agents').exists())

    def test_missing_companion_blocks(self):
        e=self.final(); (self.repo/'agents'/e['files']['research']).unlink()
        with self.assertRaisesRegex(c.Error,'missing regular file'): self.valid()

    def test_orphan_blocks(self):
        self.final(); (self.repo/'agents/inputs/orphan.md').write_text('orphan')
        with self.assertRaisesRegex(c.Error,'orphan'): self.valid()

    def test_symlink_escape_blocks_without_touching_target(self):
        outside=self.repo/'outside'; outside.mkdir(); (outside/'sentinel').write_text('keep')
        (self.repo/'agents').symlink_to(outside,target_is_directory=True)
        with self.assertRaisesRegex(c.Error,'symlink'): self.create()
        self.assertEqual(list(outside.iterdir()),[outside/'sentinel'])

    def test_symlink_companion_blocks(self):
        e=self.final(); p=self.repo/'agents'/e['files']['input']; p.unlink()
        p.symlink_to(self.repo/'.gitignore')
        with self.assertRaisesRegex(c.Error,'symlink'): self.valid()

    def test_fifo_rejected_without_blocking(self):
        self.final(); os.mkfifo(self.repo/'agents/fifo')
        with self.assertRaisesRegex(c.Error,'regular file'): self.valid()

    def test_secret_input_rejected_before_creation(self):
        with self.assertRaisesRegex(c.Error,'credential'):
            self.create(text='synthetic token: '+'ghp_'+'a'*36)
        self.assertFalse((self.repo/'agents').exists())

    def test_secret_in_unindexed_file_is_scanned(self):
        self.final(); (self.repo/'agents/extra.md').write_text('sk-'+'b'*32)
        with self.assertRaisesRegex(c.Error,'credential'): self.valid()

    def test_bound_large_file(self):
        e=self.final(); (self.repo/'agents'/e['files']['input']).write_bytes(b'x'*(c.MAX_FILE+1))
        with self.assertRaisesRegex(c.Error,'budget'): self.valid()

    def test_no_ignore_rule_blocks_writes(self):
        (self.repo/'.gitignore').write_text('')
        with self.assertRaises(c.Error): self.create()
        self.assertFalse((self.repo/'agents').exists())

    def test_duplicate_json_keys_rejected(self):
        self.create(); (self.repo/'agents/history.json').write_text('{"format":1,"format":2,"events":[]}')
        with self.assertRaisesRegex(c.Error,'duplicate JSON key'): c.load(self.repo)

    def test_failed_index_write_rolls_back_only_new_files(self):
        e=self.final(); previous=(self.repo/'agents/history.json').read_bytes()
        with patch.object(c,'store',side_effect=OSError('simulated write failure')):
            with self.assertRaises(OSError): self.create(conversation=e['conversation_id'])
        self.assertEqual((self.repo/'agents/history.json').read_bytes(),previous)
        self.assertEqual(self.valid(),1)

    def test_failed_finalize_preserves_draft(self):
        e=self.create(); self.fill(e)
        with patch.object(c,'store',side_effect=OSError('simulated replace failure')):
            with self.assertRaises(OSError): c.finalize(self.repo,e['event_id'],'working-tree')
        self.assertEqual(c.load(self.repo)['events'][0]['status'],'draft')
        c.finalize(self.repo,e['event_id'],'working-tree'); self.valid()

    def test_concurrent_processes_allocate_distinct_sequences(self):
        e=self.create(); src=self.repo/'prompt.txt'; src.write_text('Concurrent fixture.')
        cmd=[sys.executable,str(SCRIPTS/'collab.py'),'new',str(self.repo),'test-concurrency',
             '--agent','Test','--role','analyst','--conversation',e['conversation_id'],'--input-file',str(src)]
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            results=list(pool.map(lambda _:subprocess.run(cmd,capture_output=True,text=True,timeout=15),range(2)))
        self.assertTrue(all(r.returncode==0 for r in results),[r.stderr for r in results])
        self.assertEqual([e['sequence'] for e in c.load(self.repo)['events']],[1,2,3])
        self.assertEqual(self.valid(drafts=True),3)

    def test_generic_research_without_ai_sections_is_rejected(self):
        e=self.create(); self.fill(e); p=self.repo/'agents'/e['files']['research']
        p.write_text(p.read_text().replace('## AI Concept','## Project findings'))
        with self.assertRaisesRegex(c.Error,'AI Concept'): c.finalize(self.repo,e['event_id'],'working-tree')

    def test_duplicate_commands_rejected(self):
        e=self.create(); self.fill(e); p=self.repo/'agents'/e['files']['commands']
        meta,_=c.engine.parse_document(p.read_text())
        block='## `git status`\n- **What:** Inspect state.\n- **Why:** Verify changes.\n'
        p.write_text(c.engine.render_document(meta,'# Command inventory\n\n'+block+block))
        with self.assertRaisesRegex(c.Error,'duplicate'): c.finalize(self.repo,e['event_id'],'working-tree')

    def test_finalization_cannot_reseal_a_final(self):
        e=self.final()
        with self.assertRaisesRegex(c.Error,'only v2 drafts'): c.finalize(self.repo,e['event_id'],'working-tree')

    def test_fingerprint_changes_with_worktree(self):
        a=c.engine.repository_identity(self.repo,'working-tree')[2]
        (self.repo/'new-doc.md').write_text('A new document.\n')
        b=c.engine.repository_identity(self.repo,'working-tree')[2]
        self.assertNotEqual(a,b)

    def test_unknown_schema_field_rejected(self):
        self.create(); self.mutate_doc(lambda d:d['events'][0].update(ignored_flag=True))
        with self.assertRaisesRegex(c.Error,'event fields'): self.valid(drafts=True)

    def test_hard_link_rejected(self):
        e=self.final(); p=self.repo/'agents'/e['files']['input']
        os.link(p,self.repo/'input-hardlink')
        with self.assertRaisesRegex(c.Error,'hard-linked'): self.valid()

    def test_missing_root_rejected(self):
        self.assertEqual(c.main(['lint',str(self.repo/'absent')]),1)

    def test_path_traversal_in_index_rejected(self):
        self.create(); self.mutate_doc(lambda d:d['events'][0]['files'].update(input='../outside'))
        with self.assertRaisesRegex(c.Error,'companion paths'): self.valid(drafts=True)


if __name__=='__main__': unittest.main()
