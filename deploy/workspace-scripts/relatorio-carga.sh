#!/bin/sh
# Relatório de carga diário do Kuromatsu (AC-028-8, ADR-020 P5) — 0 tokens.
# Roda como job de cron `command`; a saída vai para o canal configurado no job.
# Instalar em <workspace>/scripts/ (o guard do exec bloqueia caminhos fora do workspace
# na linha de comando; o script em si lê a telemetria e o /proc).
# Só usa python3 da biblioteca padrão (a VM não tem o CLI sqlite3).
#   KUROMATSU_TURNS_DB  caminho do turns.db (default: /var/lib/kuromatsu/telemetry/turns.db)
#   RELATORIO_HORAS     janela em horas (default: 24)
exec python3 - "$@" <<'PY'
import os, sqlite3

db = os.environ.get("KUROMATSU_TURNS_DB", "/var/lib/kuromatsu/telemetry/turns.db")
hours = int(os.environ.get("RELATORIO_HORAS", "24"))
out = [f"Carga das últimas {hours} h"]


def read(path):
    try:
        with open(path) as f:
            return f.read().strip()
    except OSError:
        return None


try:
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    since = f"-{hours} hours"
    rows = con.execute(
        """SELECT origin, COUNT(*), AVG(prompt_tokens), SUM(cached_tokens) * 100.0 / MAX(SUM(prompt_tokens), 1),
                  AVG(output_tokens), SUM(total_ms) / 3600000.0, MAX(total_ms) / 60000.0,
                  SUM(status != 'completed'), AVG(steal_pct)
           FROM turns WHERE ts >= datetime('now', ?) GROUP BY origin ORDER BY 6 DESC""",
        (since,),
    ).fetchall()
    if not rows:
        out.append("turnos: nenhum")
    total_h = 0.0
    for o, n, p, c, g, h, mx, err, st in rows:
        total_h += h or 0
        out.append(
            f"{o or '?'}: {n} turnos | prompt {p or 0:.0f} ({c or 0:.0f}% cache) | saída {g or 0:.0f}"
            f" | CPU {h or 0:.1f} h (máx {mx or 0:.0f} min) | erros {err} | steal {st or 0:.1f}%"
        )
    if rows:
        out.append(f"total LLM: {total_h:.1f} h de {hours} h")
    con.close()
except sqlite3.Error as e:
    out.append(f"turns.db indisponível: {e}")

# Limite/uso reais da cgroup do serviço (mesmo caminho que AC-018-7 passa a usar).
cg = read("/proc/self/cgroup")
if cg and cg.startswith("0::"):
    base = "/sys/fs/cgroup" + cg.split("::", 1)[1]
    mib = lambda v: f"{int(v) // 1048576} MiB" if v and v.isdigit() else (v or "?")
    out.append(f"cgroup: atual {mib(read(base + '/memory.current'))} | pico {mib(read(base + '/memory.peak'))}"
               f" | limite {mib(read(base + '/memory.max'))}")

mi = read("/proc/meminfo")
if mi:
    kv = {l.split(":")[0]: l.split()[1] for l in mi.splitlines() if ":" in l}
    out.append(f"MemAvailable: {int(kv.get('MemAvailable', 0)) // 1024} MiB")

psi = read("/proc/pressure/memory")
if psi:
    some = psi.splitlines()[0].split()
    out.append("PSI memória (some): " + " ".join(some[1:4]))

print("\n".join(out))
PY
