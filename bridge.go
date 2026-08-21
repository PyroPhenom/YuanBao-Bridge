package main

import (
	"encoding/json"
	"strings"
)

// Shared page-side helpers for model resolution and SSE parsing.
const chatHelpersJS = `
  async function resolveDeepThinkingModel(svc, agentId, baseModelId) {
    let models = [];
    try {
      if (typeof svc.getModelList === 'function') {
        const cached = await svc.getModelList();
        if (Array.isArray(cached)) models = cached;
      }
    } catch (e) {}
    if (!models.length) {
      const raw = await svc.request({
        url: '/api/agent/model/list',
        method: 'POST',
        data: { agentId },
      });
      const list = raw?.modelList || raw?.data?.modelList || raw?.data || raw || [];
      models = Array.isArray(list) ? list : [];
    }
    let parent = models.find((m) => m?.chatModelId === baseModelId);
    if (!parent) {
      parent = models.find((m) =>
        (m?.subModelList || []).some((s) => s?.chatModelId === baseModelId)
      );
    }
    if (!parent) {
      return { ok: false, error: 'model not found in model list: ' + baseModelId };
    }
    const sub = (parent.subModelList || [])[0];
    if (!sub?.chatModelId) {
      return { ok: false, error: 'model has no thinking subModel: ' + baseModelId };
    }
    const internetSearch =
      parent.internetSearch?.chooseDefault || 'closeInternetSearch';
    return {
      ok: true,
      chatModelId: sub.chatModelId,
      chatModelExtInfo: JSON.stringify({
        modelId: parent.chatModelId,
        subModelId: sub.chatModelId,
        supportFunctions: { internetSearch },
      }),
    };
  }

  const THINK_CONTENT_TYPES = new Set([
    'think',
    'thinking',
    'reasoning',
    'deep_think',
    'deepThink',
    'DEEP_THINK',
  ]);

  function collectFromEvents(events) {
    let text = '';
    let thinking = '';
    for (const ev of events || []) {
      if (ev?.type === 'error') return { ok: false, error: ev.msg || ev, events };
      if (ev?.type === 'think' && ev.msg) thinking += ev.msg;
      if (ev?.type === 'deepSearch' && Array.isArray(ev.contents)) {
        const title = ev.title ? String(ev.title) : '';
        const isThinking = title.indexOf('思考') >= 0 || ev.iconType === 9;
        if (isThinking) {
          for (const c of ev.contents) {
            if (c?.msg) thinking += c.msg;
          }
        }
      }
      if (ev?.type === 'text' && ev.msg) text += ev.msg;
      const speeches = ev.speechesV2 || ev.speeches || [];
      for (const sp of speeches) {
        for (const c of sp.content || []) {
          if (!c) continue;
          if (c.type === 'text' && c.msg) text += c.msg;
          else if (c.msg && THINK_CONTENT_TYPES.has(c.type)) thinking += c.msg;
        }
      }
    }
    return {
      ok: !!(text || thinking),
      response_text: text,
      thinking_text: thinking,
      events,
    };
  }
`

const chatJS = `
(async () => {
  const prompt = __PROMPT__;
  const agentId = __AGENT__;
  const model = __MODEL__;
  const existingConvId = __CONV_ID__;
  const thinkingEnabled = __THINKING__;

` + chatHelpersJS + `

  let svc = null;
  await new Promise((resolve) => {
    window.webpackChunk_N_E.push([
      [Date.now()],
      {},
      (require) => {
        try {
          const mod = require(410682);
          svc = mod?.W || mod?.default?.W || mod?.default;
        } catch (e) {}
        resolve();
      },
    ]);
  });

  if (!svc || typeof svc.request !== 'function') {
    return { ok: false, error: 'webpack module 410682.W (ybChatService) not found' };
  }

  let convId = existingConvId;
  if (!convId) {
    const create = await svc.request({
      url: '/api/user/agent/conversation/create',
      method: 'POST',
      data: { agentId },
    });
    convId = create?.id || create?.data?.id;
    if (!convId) return { ok: false, error: 'create conversation failed', create };
  }

  let chatModelId = model;
  const body = {
    model: 'gpt_175B_0404',
    prompt,
    plugin: 'Adaptive',
    displayPrompt: prompt,
    displayPromptType: 1,
    options: {
      imageIntention: {
        needIntentionModel: true,
        backendUpdateFlag: 2,
        intentionStatus: true,
      },
    },
    multimedia: [],
    agentId,
    supportHint: 1,
    version: 'v2',
    chatModelId,
  };

  if (thinkingEnabled) {
    const cfg = await resolveDeepThinkingModel(svc, agentId, model);
    if (!cfg.ok) return { ok: false, error: cfg.error, convId };
    chatModelId = cfg.chatModelId;
    body.chatModelId = chatModelId;
    body.chatModelExtInfo = cfg.chatModelExtInfo;
  }

  const resp = await svc.request({
    url: '/api/chat/' + convId,
    method: 'POST',
    data: body,
    responseType: 'stream',
    headers: { Accept: 'text/event-stream' },
  });

  if (typeof resp === 'string') {
    const events = [];
    for (const line of resp.split('\n')) {
      const t = line.trim();
      if (!t.startsWith('data:')) continue;
      try { events.push(JSON.parse(t.slice(5))); } catch (e) {}
    }
    const parsed = collectFromEvents(events);
    return { ...parsed, convId, chatModelId };
  }

  if (resp && typeof resp === 'object') {
    if (resp.type === 'error') {
      return { ok: false, convId, error: resp.msg || resp, raw: resp };
    }
    if (resp.response_text || resp.text || resp.content) {
      return {
        ok: true,
        convId,
        chatModelId,
        response_text: resp.response_text || resp.text || resp.content,
        raw: resp,
      };
    }
    if (Array.isArray(resp.events)) {
      const parsed = collectFromEvents(resp.events);
      return { ...parsed, convId, chatModelId };
    }
    if (resp.reader || resp.getReader) {
      const reader = resp.reader || resp.getReader();
      const dec = new TextDecoder();
      let buf = '';
      const events = [];
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let idx;
        while ((idx = buf.indexOf('\n')) >= 0) {
          const line = buf.slice(0, idx).trim();
          buf = buf.slice(idx + 1);
          if (!line.startsWith('data:')) continue;
          try { events.push(JSON.parse(line.slice(5))); } catch (e) {}
        }
      }
      const parsed = collectFromEvents(events);
      return { ...parsed, convId, chatModelId };
    }
  }

  return { ok: false, convId, error: 'unexpected response shape', rawType: typeof resp, raw: resp };
})()
`

type ChatResult struct {
	OK             bool   `json:"ok"`
	Response       string `json:"response,omitempty"`
	Thinking       string `json:"thinking,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Error          string `json:"error,omitempty"`
	Raw            any    `json:"raw,omitempty"`
}

func mustJSON(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `"` + v + `"`
	}
	return string(b)
}

func convIDJSON(id string) string {
	if strings.TrimSpace(id) == "" {
		return "null"
	}
	return mustJSON(id)
}

func thinkingJSON(enabled bool) string {
	if enabled {
		return "true"
	}
	return "false"
}

func normalizeChatResult(raw map[string]interface{}) ChatResult {
	convID := strVal(raw, "convId", "conv_id")
	text := strVal(raw, "response_text", "response")
	thinking := strVal(raw, "thinking_text", "thinking")
	if okBool(raw["ok"]) {
		return ChatResult{
			OK:             true,
			Response:       text,
			Thinking:       thinking,
			ConversationID: convID,
			Raw:            raw,
		}
	}
	errMsg := strVal(raw, "error", "msg")
	if errMsg == "" {
		errMsg = "chat failed"
	}
	return ChatResult{
		OK:             false,
		Error:          errMsg,
		Thinking:       thinking,
		ConversationID: convID,
		Raw:            raw,
	}
}

func strVal(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func okBool(v interface{}) bool {
	b, ok := v.(bool)
	return ok && b
}
