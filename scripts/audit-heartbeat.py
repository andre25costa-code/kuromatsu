"""Read-only heartbeat diagnostics; omit credentials, prompts and chat contents."""
import json
from pathlib import Path
import re
import subprocess

cfg = json.loads(Path('/var/lib/kuromatsu/config.json').read_text())
defaults = cfg.get('agents', {}).get('defaults', {})
for key in ('context_window', 'max_tokens', 'max_tool_iterations', 'focus'):
    print('DEFAULT', key, json.dumps(defaults.get(key)))
for key in ('heartbeat', 'native'):
    print('CONFIG', key, json.dumps(cfg.get(key)))
for model in cfg.get('model_list', []):
    print('MODEL', {k:v for k,v in model.items() if k in ('model_name','model','provider','context_window','max_tokens','native','tool_schema_transform')})
    print('NATIVE_OPTIONS', {k:v for k,v in model.get('extra_body', {}).items() if k in ('n_ctx','n_threads','n_batch','max_predict','kv_cache_type','core_cache_parking')})
workspace = Path(defaults['workspace'])
for name in ('HEARTBEAT.md','AGENT.md','AGENTS.md','SOUL.md','USER.md','IDENTITY.md','memory/MEMORY.md'):
    path = workspace / name
    if path.exists():
        print('FILE', name, 'bytes', path.stat().st_size, 'mtime', path.stat().st_mtime)
result = subprocess.run(['journalctl','-u','kuromatsu','--since','2026-09-16 18:00:00','--no-pager','-o','cat'], capture_output=True,text=True,check=True)
lines = re.sub(r'\x1b\[[0-9;]*m', '', result.stdout).splitlines()
for line in lines:
    if ' WRN localllm ' in line and ('prompt exceeds context window' in line or 'context window filled' in line):
        print('ENGINE_WARNING', line[:700])
for line in lines:
    if 'prompt exceeds context window' in line and ('prompt_tokens' in line or 'n_ctx' in line):
        print('OVERFLOW', re.sub(r'\x1b\[[0-9;]*m','',line))
print('OVERFLOW_COUNT', sum('context_length_exceeded' in line for line in lines))
completed = [line for line in lines if ' INF localllm ' in line and '> completion ' in line]
for line in completed[-8:]:
    print('COMPLETION', re.findall(r'(?:prefill_ms|gen_ms|cached_tokens|prompt_tokens|output_tokens|n_threads|gen_tok_s)=[0-9.]+', line))

