"""Read only new heartbeat results after a deployment's logged byte offset."""
from pathlib import Path
import re
import subprocess
import sys
import time

offset = int(sys.argv[1])
log = Path('/var/lib/kuromatsu/workspace/heartbeat.log')
with log.open('rb') as source:
    source.seek(offset)
    lines = source.read().decode(errors='replace').splitlines()
for line in lines:
    if 'Heartbeat OK - silent' in line:
        print(line)
    elif 'Heartbeat completed:' in line:
        print(line[:21], 'HEARTBEAT_COMPLETED')
    elif 'Heartbeat error:' in line:
        print('HEARTBEAT_ERROR', 'context_overflow' if 'context_length_exceeded' in line else 'other')
    elif 'Resolved channel:' in line:
        print(line[:21], 'HEARTBEAT_STARTED')
print('SERVICE', subprocess.check_output(['systemctl','is-active','kuromatsu'],text=True).strip())
invocation = subprocess.check_output(['systemctl','show','kuromatsu','-p','InvocationID','--value'],text=True).strip()
journal = subprocess.check_output(['journalctl','--no-pager','-o','cat',f'_SYSTEMD_INVOCATION_ID={invocation}'],text=True)
journal = re.sub(r'\x1b\[[0-9;]*m', '', journal)
completed = [line for line in journal.splitlines() if ' INF localllm ' in line and '> completion ' in line]
print('NATIVE_COMPLETED_CALLS', len(completed))
if completed:
    print('LAST_NATIVE_METRICS', re.findall(r'(?:prefill_ms|gen_ms|cached_tokens|prompt_tokens|output_tokens|gen_tok_s)=[0-9.]+', completed[-1]))
print('NEW_CONTEXT_ERRORS', journal.count('context_length_exceeded'))
if len(sys.argv) > 2 and sys.argv[2] == 'wait':
    deadline = time.monotonic() + 1800
    while time.monotonic() < deadline:
        with log.open('rb') as source:
            source.seek(offset)
            recent = source.read().decode(errors='replace').splitlines()
        for line in recent:
            if 'Heartbeat OK - silent' in line:
                print(line, flush=True)
                sys.exit(0)
            if 'Heartbeat completed:' in line:
                print(line[:21], 'HEARTBEAT_COMPLETED', flush=True)
                sys.exit(0)
            if 'Heartbeat error:' in line:
                print('HEARTBEAT_ERROR', 'context_overflow' if 'context_length_exceeded' in line else 'other', flush=True)
                sys.exit(1)
        print('WAITING_FOR_HEARTBEAT', flush=True)
        time.sleep(30)
    print('HEARTBEAT_OBSERVATION_TIMEOUT', flush=True)
    sys.exit(2)
