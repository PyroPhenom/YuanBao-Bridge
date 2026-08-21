package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// resolveChatContextJS reads the active Yuanbao chat page state via CDP.
const resolveChatContextJS = `
(async () => {
  const hints = __HINTS__;

  function pickId(v) {
    if (v == null) return '';
    if (typeof v !== 'string') v = String(v);
    v = v.trim();
    if (!v || v === 'null' || v === 'undefined') return '';
    return v;
  }

  function scanObject(obj, depth) {
    if (!obj || typeof obj !== 'object' || depth > 5) return { agentId: '', model: '' };
    let agentId = pickId(obj.agentId) || pickId(obj.agent_id) || pickId(obj.agentID);
    let model = pickId(obj.modelId) || pickId(obj.model) || pickId(obj.chatModelId) || pickId(obj.chat_model_id);
    if (agentId && model) return { agentId, model };
    for (const k of Object.keys(obj)) {
      const v = obj[k];
      if (!v || typeof v !== 'object') continue;
      const nested = scanObject(v, depth + 1);
      agentId = agentId || nested.agentId;
      model = model || nested.model;
      if (agentId && model) break;
    }
    return { agentId, model };
  }

  function readJSONStorage(store, key) {
    try {
      const raw = store.getItem(key);
      if (!raw) return null;
      return JSON.parse(raw);
    } catch (e) {
      return null;
    }
  }

  function pickDefaultModel(models, preferred) {
    if (!Array.isArray(models) || !models.length) return '';
    if (preferred) {
      const hit = models.find((m) => m?.chatModelId === preferred || m?.modelId === preferred);
      if (hit) return pickId(hit.chatModelId || hit.modelId);
      const subHit = models.find((m) =>
        (m?.subModelList || []).some((s) => s?.chatModelId === preferred)
      );
      if (subHit) return preferred;
    }
    const chosen =
      models.find((m) => m?.chooseDefault || m?.isDefault || m?.selected) ||
      models.find((m) => m?.chatModelId) ||
      models[0];
    return pickId(chosen?.chatModelId || chosen?.modelId || chosen?.id);
  }

  function agentIdFromKeyName(key) {
    if (!key || !key.includes(':')) return '';
    const lower = key.toLowerCase();
    if (!lower.includes('agent')) return '';
    const suffix = key.slice(key.lastIndexOf(':') + 1);
    if (!suffix || suffix.length < 6 || suffix.length > 64) return '';
    if (!/^[A-Za-z0-9_]+$/.test(suffix)) return '';
    return suffix;
  }

  function scanStorageKeys(store) {
    const out = { agentId: '', model: '' };
    for (let i = 0; i < store.length; i++) {
      const key = store.key(i);
      if (!key) continue;
      if (!out.agentId) {
        const fromKey = agentIdFromKeyName(key);
        if (fromKey) out.agentId = fromKey;
      }
      if (key.endsWith('_chatModelExtInfo')) {
        const data = readJSONStorage(store, key);
        if (data) {
          if (!out.model) out.model = pickId(data.modelId) || pickId(data.subModelId);
          if (!out.agentId) out.agentId = pickId(data.agentId);
        }
      }
      if (key.endsWith('_modelList')) {
        const data = readJSONStorage(store, key);
        if (data) {
          const found = scanObject(data, 0);
          if (!out.agentId) out.agentId = found.agentId;
          if (!out.model) out.model = found.model;
        }
      }
    }
    return out;
  }

  const sources = {};
  let agentId = pickId(hints.agentId);
  let model = pickId(hints.model);

  try {
    const url = new URL(location.href);
    if (!agentId) {
      agentId = pickId(url.searchParams.get('agentId') || url.searchParams.get('agent_id'));
      if (agentId) sources.agentId = 'url';
    }
    if (!model) {
      model = pickId(url.searchParams.get('model') || url.searchParams.get('chatModelId'));
      if (model) sources.model = 'url';
    }
  } catch (e) {}

  for (const store of [localStorage, sessionStorage]) {
    const scanned = scanStorageKeys(store);
    if (scanned.agentId && !agentId) {
      agentId = scanned.agentId;
      sources.agentId = (store === localStorage ? 'localStorage:' : 'sessionStorage:') + 'key-scan';
    }
    if (scanned.model && !model) {
      model = scanned.model;
      sources.model = (store === localStorage ? 'localStorage:' : 'sessionStorage:') + 'chatModelExtInfo';
    }
  }

  const storageKeys = [
    'launch-box-model-id',
    'launch-box-model-select-data',
    'launch-box-cross-params',
    'launch-box-main-model-sync',
    'launch_box_more_actions_params',
  ];
  for (const store of [localStorage, sessionStorage]) {
    for (const key of storageKeys) {
      if (key === 'launch-box-model-id' && !model) {
        model = pickId(store.getItem(key));
        if (model) sources.model = (store === localStorage ? 'localStorage:' : 'sessionStorage:') + key;
      }
      const data = readJSONStorage(store, key);
      if (!data) continue;
      const found = scanObject(data, 0);
      if (found.agentId && !agentId) {
        agentId = found.agentId;
        sources.agentId = (store === localStorage ? 'localStorage:' : 'sessionStorage:') + key;
      }
      if (found.model && !model) {
        model = found.model;
        sources.model = (store === localStorage ? 'localStorage:' : 'sessionStorage:') + key;
      }
    }
  }

  if (!agentId) {
    for (const store of [localStorage, sessionStorage]) {
      for (let i = 0; i < store.length; i++) {
        const key = store.key(i);
        const raw = store.getItem(key);
        if (!raw || raw.length > 200000 || (!raw.includes('agentId') && !raw.includes('agent_id'))) continue;
        const data = readJSONStorage(store, key);
        if (!data) continue;
        const found = scanObject(data, 0);
        if (found.agentId) {
          agentId = found.agentId;
          sources.agentId = (store === localStorage ? 'localStorage:' : 'sessionStorage:') + key;
          break;
        }
      }
      if (agentId) break;
    }
  }

  try {
    const nextData = scanObject(window.__NEXT_DATA__, 0);
    if (nextData.agentId && !agentId) {
      agentId = nextData.agentId;
      sources.agentId = '__NEXT_DATA__';
    }
    if (nextData.model && !model) {
      model = nextData.model;
      sources.model = '__NEXT_DATA__';
    }
  } catch (e) {}

  let svc = null;
  await new Promise((resolve) => {
    window.webpackChunk_N_E.push([[Date.now()], {}, (require) => {
      try {
        const mod = require(410682);
        svc = mod?.W || mod?.default?.W || mod?.default;
      } catch (e) {}
      resolve();
    }]);
  });

  let models = [];
  if (svc && typeof svc.request === 'function') {
    try {
      if (typeof svc.getModelList === 'function') {
        const cached = await svc.getModelList();
        if (Array.isArray(cached)) models = cached;
      }
    } catch (e) {}

    if (agentId && !models.length) {
      try {
        const raw = await svc.request({
          url: '/api/agent/model/list',
          method: 'POST',
          data: { agentId },
        });
        const list = raw?.modelList || raw?.data?.modelList || raw?.data || raw || [];
        models = Array.isArray(list) ? list : [];
      } catch (e) {}
    }

    if (!agentId && models.length) {
      const withAgent = models.find((m) => m?.agentId);
      if (withAgent) {
        agentId = pickId(withAgent.agentId);
        sources.agentId = sources.agentId || 'modelList';
      }
    }

    if (!model && models.length) {
      model = pickDefaultModel(models, hints.model);
      if (model) sources.model = sources.model || 'modelList';
    }
  }

  return {
    ok: !!(agentId && model),
    agentId,
    model,
    sources,
    modelsCount: models.length,
  };
})()
`

type ChatContext struct {
	AgentID string
	Model   string
	Sources map[string]string
}

type contextCache struct {
	ctx     ChatContext
	expires time.Time
}

func (c *CDPClient) DetectChatContext(hints ChatContext) (ChatContext, error) {
	c.cacheMu.Lock()
	if c.cached != nil && time.Now().Before(c.cached.expires) {
		ctx := c.cached.ctx
		c.cacheMu.Unlock()
		return mergeChatContext(hints, ctx), nil
	}
	c.cacheMu.Unlock()

	target, err := findChatTarget(c.cfg)
	if err != nil {
		return ChatContext{}, err
	}

	hintsJSON, err := json.Marshal(map[string]string{
		"agentId": hints.AgentID,
		"model":   hints.Model,
	})
	if err != nil {
		return ChatContext{}, err
	}
	js := strings.Replace(resolveChatContextJS, "__HINTS__", string(hintsJSON), 1)

	raw, err := c.evaluateOnTarget(target, js, 20*time.Second)
	if err != nil {
		return ChatContext{}, err
	}
	if !okBool(raw["ok"]) {
		errMsg := strVal(raw, "error")
		if errMsg == "" {
			errMsg = "chat context not available on page"
		}
		return ChatContext{}, fmt.Errorf("%s", errMsg)
	}

	ctx := ChatContext{
		AgentID: strVal(raw, "agentId", "agent_id"),
		Model:   strVal(raw, "model", "modelId", "chatModelId"),
	}
	if src, ok := raw["sources"].(map[string]interface{}); ok {
		ctx.Sources = map[string]string{}
		for k, v := range src {
			if s, ok := v.(string); ok {
				ctx.Sources[k] = s
			}
		}
	}
	if ctx.AgentID == "" || ctx.Model == "" {
		return ChatContext{}, fmt.Errorf("incomplete chat context from page (agentId=%q model=%q)", ctx.AgentID, ctx.Model)
	}

	c.cacheMu.Lock()
	c.cached = &contextCache{ctx: ctx, expires: time.Now().Add(30 * time.Second)}
	c.cacheMu.Unlock()

	return mergeChatContext(hints, ctx), nil
}

func (c *CDPClient) InvalidateContextCache() {
	c.cacheMu.Lock()
	c.cached = nil
	c.cacheMu.Unlock()
}

func mergeChatContext(hints, detected ChatContext) ChatContext {
	out := detected
	if strings.TrimSpace(hints.AgentID) != "" {
		out.AgentID = strings.TrimSpace(hints.AgentID)
	}
	if strings.TrimSpace(hints.Model) != "" {
		out.Model = strings.TrimSpace(hints.Model)
	}
	return out
}
