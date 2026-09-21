"""Trace the reported exec message without exposing unrelated conversations."""
import json
from pathlib import Path
import re
import subprocess

cfg = json.loads(Path('/var/lib/kuromatsu/config.json').read_text())
workspace = Path(cfg['agents']['defaults']['workspace'])
for path in (workspace / 'sessions').rglob('*a7941f53305cdf3d*'):
    if not path.is_file():
        continue
    if '1928dab6' not in path.name:
        continue
    print('SESSION_FILE', path.name)
    raw = path.read_text()
    try:
        records = [json.loads(raw)]
    except json.JSONDecodeError:
        records = [json.loads(line) for line in raw.splitlines() if line.strip()]
    def inspect(value):
        if isinstance(value, dict):
            if value.get('role') == 'tool':
                content = value.get('content', '')
                if isinstance(content, str) and any(term in content.lower() for term in ('not found','blocked','required','not allowed')):
                    print('TOOL_ERROR_RESULT', content[:400])
            for call in value.get('tool_calls', []):
                function = call.get('function', call)
                args = function.get('arguments', {})
                if isinstance(args, str):
                    try:
                        args = json.loads(args)
                    except json.JSONDecodeError:
                        args = {}
                print('TOOL_CALL', function.get('name'), {k:args.get(k) for k in ('action','sessionId')})
            for child in value.values():
                inspect(child)
        elif isinstance(value, list):
            for child in value:
                inspect(child)
    inspect(records)
jobs = json.loads((workspace / 'cron/jobs.json').read_text())
for job in jobs.get('jobs', []):
    if job.get('id') == 'a7941f53305cdf3d':
        print('CRON_JOB', {k:job.get(k) for k in ('id','name','enabled','schedule')})
        payload = job.get('payload', {})
        print('PAYLOAD', {k:payload.get(k) for k in ('kind','deliver','agent_id')})
needle = 'exec command is not found'
for line in (workspace / 'heartbeat.log').read_text().splitlines():
    if needle.lower() in line.lower():
        print('MATCH_HEARTBEAT', line[:21], 'completed=' + str('Heartbeat completed:' in line))
print('EXEC_POLICY', {key: cfg.get('tools', {}).get('exec', {}).get(key) for key in ('enabled', 'allow_remote')})
log = subprocess.check_output(['journalctl','-u','kuromatsu','--since','2026-09-17 18:00:00','--no-pager','-o','cat'], text=True)
lines = re.sub(r'\x1b\[[0-9;]*m', '', log).splitlines()
for line in lines:
    if 'a7941f53305cdf3d-1928dab6' in line and any(k in line for k in ('tool=', 'tool_name=', 'tool_name:', 'tool.skipped')):
        print('TOOL_EVENT', line[:8], re.findall(r'(?:tool|tool_name|event_kind|stage|reason|error)=(?:"[^"]*"|[^ ]+)', line))
for line in lines:
    if needle.lower() in line.lower() or ('exec' in line and any(word in line.lower() for word in ('denied','not found','not allowed','outside','blocked'))):
        fields = re.findall(r'(?:sender_id|session_key|tool|tool_name|event_kind|stage|reason)=(?:"[^"]*"|[^ ]+)', line)
        print('JOURNAL_MATCH', line[:8], fields, 'reported_text=' + str(needle.lower() in line.lower()))
        if not (needle.lower() in line.lower()):
            print('DIAGNOSTIC', line.split(' > ', 1)[-1][:350])
