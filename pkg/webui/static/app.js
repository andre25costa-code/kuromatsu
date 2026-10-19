'use strict';
// Kuromatsu local configuration page. No framework: every value from the
// server is inserted with textContent, never as HTML.
(function () {
  var csrf = '';
  var $ = function (id) { return document.getElementById(id); };
  var msgEl = $('msg');

  function say(text, isErr) {
    msgEl.textContent = text;
    msgEl.className = isErr ? 'err' : '';
  }

  function el(tag, text, cls) {
    var e = document.createElement(tag);
    if (text != null) e.textContent = text;
    if (cls) e.className = cls;
    return e;
  }

  function btn(text, fn) {
    var b = el('button', text, 'sec');
    b.type = 'button';
    b.addEventListener('click', fn);
    return b;
  }

  function api(method, path, body, raw) {
    var opt = { method: method, headers: {}, credentials: 'same-origin' };
    if (method !== 'GET') opt.headers['X-CSRF-Token'] = csrf;
    if (body !== undefined) {
      if (raw) {
        opt.headers['Content-Type'] = 'text/plain; charset=utf-8';
        opt.body = body;
      } else {
        opt.headers['Content-Type'] = 'application/json';
        opt.body = JSON.stringify(body);
      }
    }
    return fetch('/api/ui/' + path, opt).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (data) {
        if (r.status === 401) { showLogin(); throw new Error('Sessão expirada: entre de novo.'); }
        if (!r.ok) throw new Error(data.error || ('Erro HTTP ' + r.status));
        if (r.status === 202 && data.warning) data.note = 'Salvo; aviso: ' + data.warning;
        return data;
      });
    });
  }

  function fail(e) { say(e.message, true); }

  function saved(data) {
    say(data && data.note ? data.note : (data && data.reloading ? 'Aplicado; recarregando.' : 'Salvo.'));
    return data;
  }

  function onSubmit(id, fn) {
    $(id).addEventListener('submit', function (ev) {
      ev.preventDefault();
      say('Enviando…');
      Promise.resolve().then(fn).catch(fail);
    });
  }

  // login and navigation

  function showLogin() {
    $('app').hidden = true;
    $('logout').hidden = true;
    $('login-view').hidden = false;
    $('login-token').focus();
  }

  function showApp() {
    $('login-view').hidden = true;
    $('app').hidden = false;
    $('logout').hidden = false;
    route();
  }

  var loaders = {};
  var sections = ['assistente', 'modelos', 'canais', 'skills', 'automacoes', 'heartbeat', 'status'];

  function route() {
    var name = location.hash.slice(1);
    if (sections.indexOf(name) < 0) name = 'assistente';
    sections.forEach(function (s) { $(s).hidden = s !== name; });
    document.querySelectorAll('nav a').forEach(function (a) {
      if (a.getAttribute('href') === '#' + name) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    });
    say('');
    if (loaders[name]) loaders[name]().catch(fail);
  }

  window.addEventListener('hashchange', function () { if (!$('app').hidden) route(); });

  onSubmit('login-form', function () {
    return fetch('/api/ui/login', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: $('login-token').value.trim() })
    }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (d) {
        if (r.status === 429) throw new Error('Muitas tentativas. Espere um minuto.');
        if (!r.ok) throw new Error('Token inválido.');
        csrf = d.csrf;
        $('login-token').value = '';
        showApp();
      });
    });
  });

  $('logout').addEventListener('click', function () {
    api('POST', 'logout').then(function () { csrf = ''; showLogin(); say('Sessão encerrada.'); }).catch(fail);
  });

  // Assistente

  function loadFile(name, area) {
    return api('GET', 'workspace/' + name).then(function (d) { area.value = d.content; });
  }
  loaders.assistente = function () { return loadFile($('ws-file').value, $('ws-text')); };
  $('ws-file').addEventListener('change', function () { loaders.assistente().catch(fail); });
  onSubmit('ws-form', function () {
    return api('PUT', 'workspace/' + $('ws-file').value, $('ws-text').value, true).then(saved);
  });

  // Modelos

  loaders.modelos = function () {
    return api('GET', 'models').then(function (d) {
      var ul = $('models-list');
      ul.replaceChildren();
      d.models.forEach(function (m) {
        var li = el('li');
        li.append(el('strong', m.alias + (m.is_default ? ' (padrão)' : '')));
        li.append(el('div', (m.provider || 'openai-compatível') + ' · ' + m.model + ' · ' +
          (m.api_base || '—') + ' · ' + (m.has_key ? 'com chave' : 'sem chave'), 'meta'));
        if (!m.is_default) {
          li.append(btn('Usar como padrão', function () {
            api('PUT', 'models/default', { alias: m.alias }).then(saved).then(loaders.modelos).catch(fail);
          }));
        }
        li.append(btn('Editar', function () {
          $('m-alias').value = m.alias;
          $('m-provider').value = m.provider || '';
          $('m-base').value = m.api_base;
          $('m-model').value = m.model;
          $('m-key').value = '';
          $('m-alias').focus();
        }));
        ul.append(li);
      });
    });
  };
  $('m-probe').addEventListener('click', function () {
    say('Testando…');
    api('POST', 'models/probe', {
      provider: $('m-provider').value, base: $('m-base').value,
      key: $('m-key').value, alias: $('m-alias').value
    }).then(function (d) {
      var dl = $('m-models');
      dl.replaceChildren();
      d.models.forEach(function (id) { var o = el('option'); o.value = id; dl.append(o); });
      say('Conexão ok: ' + d.models.length + ' modelos.');
    }).catch(fail);
  });
  onSubmit('model-form', function () {
    return api('POST', 'models', {
      alias: $('m-alias').value, provider: $('m-provider').value, base: $('m-base').value,
      key: $('m-key').value, model: $('m-model').value
    }).then(function (d) { $('m-key').value = ''; saved(d); return loaders.modelos(); });
  });

  // Canais e permissões

  var channels = [];
  loaders.canais = function () {
    return Promise.all([api('GET', 'channels'), api('GET', 'permissions')]).then(function (r) {
      channels = r[0].channels;
      var box = $('channels-list');
      box.replaceChildren();
      if (!channels.length) box.append(el('p', 'Nenhum canal configurado.'));
      channels.forEach(function (c, i) {
        var f = el('form', null, 'card');
        var id = 'af-' + i;
        f.append(el('h3', c.name + ' (' + c.type + ', ' + (c.enabled ? 'ligado' : 'desligado') + ')'));
        var lab = el('label', 'allow_from (um ID por linha)');
        lab.htmlFor = id;
        var ta = el('textarea');
        ta.id = id;
        ta.rows = 3;
        ta.value = c.allow_from.join('\n');
        var b = el('button', 'Salvar');
        b.type = 'submit';
        f.append(lab, ta, b);
        f.addEventListener('submit', function (ev) {
          ev.preventDefault();
          api('PUT', 'channels/' + encodeURIComponent(c.name) + '/allow_from',
            { allow_from: ta.value.split('\n') }).then(saved).catch(fail);
        });
        box.append(f);
      });
      var p = r[1];
      if (p.preset !== 'custom') $('perm-preset').value = p.preset;
      $('perm-current').textContent = 'Atual: ' + (p.preset === 'custom' ? 'personalizado' : p.preset);
    });
  };
  onSubmit('perm-form', function () {
    return api('PUT', 'permissions', { preset: $('perm-preset').value }).then(saved).then(loaders.canais);
  });

  // Skills

  loaders.skills = function () {
    return api('GET', 'skills').then(function (d) {
      var ul = $('skills-list');
      ul.replaceChildren();
      d.skills.forEach(function (s) {
        var li = el('li');
        li.append(el('strong', s.name), el('div', s.description || '', 'meta'));
        if (s.source === 'workspace') {
          li.append(btn('Remover', function () {
            if (!confirm('Remover a skill ' + s.name + '?')) return;
            api('DELETE', 'skills/' + encodeURIComponent(s.dir)).then(saved).then(loaders.skills).catch(fail);
          }));
        }
        ul.append(li);
      });
    });
  };
  onSubmit('skill-form', function () {
    say('Instalando… pode levar até 2 minutos.');
    return api('POST', 'skills', { repo: $('skill-repo').value.trim() })
      .then(function (d) { $('skill-repo').value = ''; saved(d); return loaders.skills(); });
  });

  // Automações

  var templates = [];
  var editing = null;

  function when(ms) { return ms ? new Date(ms).toLocaleString('pt-BR') : '—'; }

  function schedText(s) {
    if (s.kind === 'at') return 'uma vez em ' + when(s.atMs);
    if (s.kind === 'every') return 'a cada ' + Math.round(s.everyMs / 60000) + ' min';
    return 'cron ' + s.expr + (s.tz ? ' (' + s.tz + ')' : '');
  }

  function act(path, method) {
    return function () {
      api(method || 'POST', path).then(function (d) {
        say(method === 'DELETE' ? 'Removida.' : (path.slice(-3) === 'run' ? 'Marcada para rodar agora.' : 'Ok.'));
        return d;
      }).then(loaders.automacoes).catch(fail);
    };
  }

  loaders.automacoes = function () {
    return Promise.all([api('GET', 'cron'), api('GET', 'cron/templates')]).then(function (r) {
      var ul = $('cron-list');
      ul.replaceChildren();
      if (!r[0].jobs.length) ul.append(el('li', 'Nenhuma automação.'));
      r[0].jobs.forEach(function (j) {
        var li = el('li');
        var base = 'cron/' + encodeURIComponent(j.id);
        li.append(el('strong', j.name));
        li.append(el('div', schedText(j.schedule) + ' · ' + j.state + ' · próxima: ' + when(j.next_run_ms) +
          (j.consecutive_failures ? ' · falhas seguidas: ' + j.consecutive_failures : ''), 'meta'));
        li.append(j.enabled ? btn('Pausar', act(base + '/pause')) : btn('Retomar', act(base + '/resume')));
        li.append(btn('Executar agora', act(base + '/run')));
        li.append(btn('Histórico', function () {
          api('GET', base + '/history').then(function (d) {
            var pre = $('cron-history');
            pre.hidden = false;
            pre.textContent = j.name + '\n' + (d.runs.length ? d.runs.map(function (x) {
              return when(x.startedAtMs) + '  ' + x.status + '  ' + x.durationMs + ' ms' + (x.error ? '  ' + x.error : '');
            }).join('\n') : 'sem execuções');
          }).catch(fail);
        }));
        li.append(btn('Editar', function () {
          editing = j;
          $('cron-edit').hidden = false;
          $('ce-name').value = j.name;
          $('ce-msg').value = j.message;
          $('ce-expr').value = j.schedule.kind === 'cron' ? j.schedule.expr : '';
          $('ce-expr').disabled = j.schedule.kind !== 'cron';
          $('ce-name').focus();
        }));
        li.append(btn('Excluir', function () {
          if (confirm('Excluir ' + j.name + '?')) act(base, 'DELETE')();
        }));
        ul.append(li);
      });
      templates = r[1].templates;
      var sel = $('cf-tpl');
      var cur = sel.value;
      sel.replaceChildren();
      templates.forEach(function (t) { var o = el('option', t.title); o.value = t.id; sel.append(o); });
      if (cur) sel.value = cur;
      var dt = r[1].default_target;
      if (!$('cf-channel').value) $('cf-channel').value = dt.channel;
      if (!$('cf-to').value) $('cf-to').value = dt.to;
      renderParams();
    });
  };

  function renderParams() {
    var t = templates.find(function (x) { return x.id === $('cf-tpl').value; });
    var box = $('cf-params');
    box.replaceChildren();
    if (!t) return;
    $('cf-desc').textContent = t.description + (t.unmet.length ? ' Requer: ' + t.unmet.join(', ') + '.' : '');
    t.params.forEach(function (p) {
      var id = 'cfp-' + p.name;
      var lab = el('label', p.label + (p.required ? ' *' : ''));
      lab.htmlFor = id;
      var inp = el('input');
      inp.id = id;
      inp.name = p.name;
      inp.required = !!p.required;
      inp.placeholder = p.kind === 'time' ? 'HH:MM' : p.kind === 'datetime' ? 'HH:MM ou 2026-01-31T09:00:00-03:00' : (p.default || '');
      if (p.kind === 'url') inp.type = 'url';
      if (p.kind === 'int') inp.type = 'number';
      box.append(lab, inp);
    });
  }
  $('cf-tpl').addEventListener('change', renderParams);

  onSubmit('cron-form', function () {
    var params = {};
    $('cf-params').querySelectorAll('input').forEach(function (i) { if (i.value.trim()) params[i.name] = i.value.trim(); });
    return api('POST', 'cron', {
      template: $('cf-tpl').value, params: params,
      channel: $('cf-channel').value, to: $('cf-to').value
    }).then(function (d) {
      say(d.created ? 'Automação criada.' : 'Já existia uma automação igual.');
      return loaders.automacoes();
    });
  });

  onSubmit('cron-edit', function () {
    var body = { name: $('ce-name').value, message: $('ce-msg').value };
    if (editing.schedule.kind === 'cron') {
      body.schedule = Object.assign({}, editing.schedule, { expr: $('ce-expr').value.trim() });
    }
    return api('PATCH', 'cron/' + encodeURIComponent(editing.id), body).then(function () {
      $('cron-edit').hidden = true;
      say('Automação atualizada.');
      return loaders.automacoes();
    });
  });
  $('ce-cancel').addEventListener('click', function () { $('cron-edit').hidden = true; });

  // Heartbeat

  loaders.heartbeat = function () {
    return Promise.all([api('GET', 'heartbeat'), loadFile('HEARTBEAT.md', $('hbf-text'))]).then(function (r) {
      $('hb-on').checked = r[0].enabled;
      $('hb-int').value = r[0].interval;
    });
  };
  onSubmit('hb-form', function () {
    return api('PUT', 'heartbeat', { enabled: $('hb-on').checked, interval: parseInt($('hb-int').value, 10) }).then(saved);
  });
  onSubmit('hbf-form', function () {
    return api('PUT', 'workspace/HEARTBEAT.md', $('hbf-text').value, true).then(saved);
  });

  // Status

  loaders.status = function () {
    return api('GET', 'status').then(function (d) {
      var lines = ['Versão: ' + d.version, d.summary, ''];
      if (d.hardware) {
        var h = d.hardware;
        lines.push('Hardware: ' + h.total_ram + ' RAM (' + h.available_ram + ' livre), ' + h.cpus + ' CPUs, ' + h.os + '/' + h.arch);
        lines.push('Perfil detectado: ' + d.profile.detected + ' · config: ' + d.profile.config);
      }
      lines.push('', d.stats_error ? 'Telemetria: ' + (/disabled/.test(d.stats_error) ? 'telemetria desligada' : d.stats_error) : d.stats);
      $('st-text').textContent = lines.join('\n');
    });
  };
  $('st-refresh').addEventListener('click', function () { loaders.status().catch(fail); });

  // start

  fetch('/api/ui/session', { credentials: 'same-origin' }).then(function (r) {
    if (!r.ok) { showLogin(); return; }
    return r.json().then(function (d) { csrf = d.csrf; showApp(); });
  }).catch(function () { showLogin(); });
})();
