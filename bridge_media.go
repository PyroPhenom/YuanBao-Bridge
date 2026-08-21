package main

import (
	"encoding/json"
	"strings"
	"time"
)

// uploadAndChatJS uploads local images via genUploadInfo + COS PUT, then chats.
const uploadAndChatJS = `
(async () => {
  const prompt = __PROMPT__;
  const agentId = __AGENT__;
  const model = __MODEL__;
  const existingConvId = __CONV_ID__;
  const imagePaths = __IMAGE_PATHS__;
  const thinkingEnabled = __THINKING__;

` + chatHelpersJS + `

  let svc = null;
  let readFile = null;
  let allowAsset = null;

  await new Promise((resolve) => {
    window.webpackChunk_N_E.push([[Date.now()], {}, (require) => {
      try { svc = require(410682).W; } catch (e) {}
      try { readFile = require(489711).readFile; } catch (e) {}
      try { allowAsset = require(328993).lA; } catch (e) {}
      resolve();
    }]);
  });

  if (!svc || typeof svc.request !== 'function') {
    return { ok: false, error: 'webpack module 410682.W (ybChatService) not found' };
  }
  if (!readFile || typeof readFile !== 'function') {
    return { ok: false, error: 'readFile (489711) not found' };
  }

  function parseResourceId(resourceUrl) {
    if (!resourceUrl) return '';
    try { return new URL(resourceUrl).searchParams.get('resourceId') || ''; } catch (e) { return ''; }
  }

  async function getImageDimensions(file) {
    const url = URL.createObjectURL(file);
    try {
      return await new Promise((resolve, reject) => {
        const img = new Image();
        img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight });
        img.onerror = () => reject(new Error('image load failed'));
        img.src = url;
      });
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  async function putToCos(cosURL, file, putAuthorization) {
    const headers = { 'Content-Type': file.type || 'application/octet-stream' };
    if (putAuthorization) headers.Authorization = putAuthorization;
    await new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('PUT', cosURL, true);
      Object.entries(headers).forEach(([k, v]) => { if (v) xhr.setRequestHeader(k, v); });
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) resolve();
        else reject(new Error('COS PUT failed: status ' + xhr.status));
      };
      xhr.onerror = () => reject(new Error('COS PUT network error'));
      xhr.send(file);
    });
  }

  async function uploadOne(localPath) {
    if (typeof allowAsset === 'function') {
      try { await allowAsset('allow_asset_file', { src_path: localPath }); } catch (e) {}
    }
    const bytes = await readFile(localPath);
    const fileName = localPath.split(/[\\/]/).pop() || 'image.png';
    const ext = (fileName.split('.').pop() || 'png').toLowerCase();
    const mime = ext === 'jpg' || ext === 'jpeg' ? 'image/jpeg'
      : ext === 'webp' ? 'image/webp'
      : ext === 'gif' ? 'image/gif'
      : 'image/png';
    const file = new File([new Blob([bytes], { type: mime })], fileName, { type: mime });

    const fileId = Math.random().toString(16).slice(2);
    let openId = '';
    try {
      const auth = JSON.parse(localStorage.getItem('oneIDAuthInfo') || '[]');
      if (auth.length) openId = auth[0].openID || '';
    } catch (e) {}

    const raw = await svc.request({
      url: '/api/resource/genUploadInfo',
      method: 'POST',
      data: {
        fileName,
        fileId,
        docFrom: 'localDoc',
        docOpenid: openId,
        needAuth: true,
        scene: 2,
      },
    });
    const info = raw && raw.data ? raw.data : raw;
    if (!info) throw new Error('genUploadInfo returned empty response');

    const cosURL = info.cosURL || info.cosUrl;
    if (!info.isUploaded && cosURL) {
      await putToCos(cosURL, file, info.putAuthorization);
    }

    const resourceUrl = info.resourceUrl || '';
    const resourceId = parseResourceId(resourceUrl) || info.resourceID || info.resourceId || '';
    let url = resourceUrl;
    if (url && !url.includes('resourceId') && resourceId) {
      url = 'https://hunyuan.tencent.com/api/resource/download?resourceId=' + resourceId;
    }

    let width = 0;
    let height = 0;
    try {
      const dim = await getImageDimensions(file);
      width = dim.width;
      height = dim.height;
    } catch (e) {}

    return {
      type: 'image',
      url: url,
      OriginUrl: '',
      fileName: fileName,
      size: file.size,
      width: width,
      height: height,
      mediaId: resourceId,
      chatRecordId: (crypto.randomUUID && crypto.randomUUID()) || String(Date.now()) + Math.random(),
      docType: 'image',
    };
  }

  const multimedia = [];
  const uploadErrors = [];
  for (const p of imagePaths) {
    try {
      multimedia.push(await uploadOne(p));
    } catch (e) {
      uploadErrors.push({ path: p, error: String(e) });
    }
  }
  if (!multimedia.length) {
    return { ok: false, error: 'all image uploads failed', uploadErrors };
  }

  let convId = existingConvId;
  if (!convId) {
    const create = await svc.request({
      url: '/api/user/agent/conversation/create',
      method: 'POST',
      data: { agentId },
    });
    convId = create?.id || create?.data?.id;
    if (!convId) return { ok: false, error: 'create conversation failed', create, uploadErrors };
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
    multimedia,
    agentId,
    supportHint: 1,
    version: 'v2',
    chatModelId,
  };

  if (thinkingEnabled) {
    const cfg = await resolveDeepThinkingModel(svc, agentId, model);
    if (!cfg.ok) return { ok: false, error: cfg.error, convId, uploadErrors };
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

  function collectText(events) {
    return collectFromEvents(events);
  }

  if (typeof resp === 'string') {
    const events = [];
    for (const line of resp.split('\n')) {
      const t = line.trim();
      if (!t.startsWith('data:')) continue;
      try { events.push(JSON.parse(t.slice(5))); } catch (e) {}
    }
    const parsed = collectText(events);
    return { ...parsed, convId, chatModelId, multimediaCount: multimedia.length, uploadErrors };
  }

  if (resp && typeof resp === 'object') {
    if (resp.type === 'error') {
      return { ok: false, convId, error: resp.msg || resp, raw: resp, uploadErrors };
    }
    if (resp.response_text || resp.text || resp.content) {
      return {
        ok: true,
        convId,
        chatModelId,
        response_text: resp.response_text || resp.text || resp.content,
        multimediaCount: multimedia.length,
        uploadErrors,
        raw: resp,
      };
    }
    if (Array.isArray(resp.events)) {
      const parsed = collectText(resp.events);
      return { ...parsed, convId, chatModelId, multimediaCount: multimedia.length, uploadErrors };
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
      const parsed = collectText(events);
      return { ...parsed, convId, chatModelId, multimediaCount: multimedia.length, uploadErrors };
    }
  }

  return { ok: false, convId, error: 'unexpected response shape', uploadErrors };
})()
`

func (c *CDPClient) ChatWithImages(prompt, agentID, model, conversationID string, imagePaths []string, thinking bool) (ChatResult, error) {
	prepared, err := prepareImagePaths(imagePaths)
	if err != nil {
		return ChatResult{OK: false, Error: err.Error()}, err
	}

	target, err := findChatTarget(c.cfg)
	if err != nil {
		return ChatResult{OK: false, Error: err.Error()}, err
	}

	pathsJSON, err := json.Marshal(prepared)
	if err != nil {
		return ChatResult{OK: false, Error: err.Error()}, err
	}

	js := uploadAndChatJS
	js = strings.Replace(js, "__PROMPT__", mustJSON(prompt), 1)
	js = strings.Replace(js, "__AGENT__", mustJSON(agentID), 1)
	js = strings.Replace(js, "__MODEL__", mustJSON(model), 1)
	js = strings.Replace(js, "__CONV_ID__", convIDJSON(conversationID), 1)
	js = strings.Replace(js, "__IMAGE_PATHS__", string(pathsJSON), 1)
	js = strings.Replace(js, "__THINKING__", thinkingJSON(thinking), 1)

	timeout := 180 * time.Second
	if thinking {
		timeout = 300 * time.Second
	}
	if len(prepared) > 1 {
		timeout = time.Duration(120+len(prepared)*60) * time.Second
	}

	raw, err := c.evaluateOnTarget(target, js, timeout)
	if err != nil {
		return ChatResult{OK: false, Error: err.Error()}, err
	}
	return normalizeChatResult(raw), nil
}

func (c *CDPClient) Chat(prompt, agentID, model, conversationID string, imagePaths []string, thinking bool) (ChatResult, error) {
	if len(imagePaths) > 0 {
		return c.ChatWithImages(prompt, agentID, model, conversationID, imagePaths, thinking)
	}
	return c.chatTextOnly(prompt, agentID, model, conversationID, thinking)
}

func (c *CDPClient) chatTextOnly(prompt, agentID, model, conversationID string, thinking bool) (ChatResult, error) {
	target, err := findChatTarget(c.cfg)
	if err != nil {
		return ChatResult{OK: false, Error: err.Error()}, err
	}

	js := chatJS
	js = strings.Replace(js, "__PROMPT__", mustJSON(prompt), 1)
	js = strings.Replace(js, "__AGENT__", mustJSON(agentID), 1)
	js = strings.Replace(js, "__MODEL__", mustJSON(model), 1)
	js = strings.Replace(js, "__CONV_ID__", convIDJSON(conversationID), 1)
	js = strings.Replace(js, "__THINKING__", thinkingJSON(thinking), 1)

	timeout := 180 * time.Second
	if thinking {
		timeout = 300 * time.Second
	}

	raw, err := c.evaluateOnTarget(target, js, timeout)
	if err != nil {
		return ChatResult{OK: false, Error: err.Error()}, err
	}
	return normalizeChatResult(raw), nil
}
