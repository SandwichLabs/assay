<script setup lang="ts">
// WatchForm — New watch / Edit watch (E7 W2). Pick a saved query (with its
// declared parameters) or a metric, a condition and a name; the YAML preview is
// the file that will be written, drawn by Go's own encoder, and every refusal is
// Go's decoder's. Saving goes through SaveWatch against the revision the form
// loaded, so a file changed meanwhile shows the conflict banner.
import { ref, toRef } from 'vue'
import Icon from './Icon.vue'
import ConflictBanner from './ConflictBanner.vue'
import { useModalKeys } from '../composables/useModal'
import type { WatchForm } from '../composables/useWatchForm'

const props = defineProps<{ form: WatchForm }>()
const root = ref<HTMLElement | null>(null)
// Focus starts on the name of a new watch (an edit's name is fixed, so its description).
useModalKeys(toRef(props.form, 'open'), () => props.form.close(), root, () => root.value?.querySelector<HTMLElement>(props.form.mode === 'new' ? '#wf-name' : '#wf-desc'))

const CONDITIONS: Array<{ value: string; label: string }> = [
  { value: 'any', label: 'Any rows come back' },
  { value: 'none', label: 'No rows come back' },
  { value: 'above', label: 'The value is above' },
  { value: 'below', label: 'The value is below' },
  { value: 'outside', label: 'The value is outside' },
]
const paramHint = (n: string) => {
  const p = props.form.specOf(n)
  if (!p) return 'Not declared by this query; the check will refuse it.'
  return `${p.type}${p.required && !p.default ? ', required' : ''}${p.default ? `, default ${p.default}` : ''}`
}
</script>

<template>
  <div v-if="form.open" class="modal-overlay" role="presentation" @click.self="form.close()">
    <form ref="root" class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="wf-h" data-testid="watch-form" @submit.prevent="form.save()">
      <div class="d-head">
        <h2 id="wf-h">{{ form.mode === 'new' ? 'New watch' : `Edit ${form.name}` }}</h2>
        <p>A watch asks a question and fires when the answer meets a condition. It is a file in <code>watches/</code>; nothing runs until you check it.</p>
      </div>

      <div v-if="form.conflict.conflict || form.replacing || form.error" class="d-alerts">
        <ConflictBanner
          v-if="form.conflict.conflict" kind="watch" :path="`watches/${form.name}.yaml`" :deleted="form.conflict.conflict.deleted" :busy="form.conflict.busy" no-copy
          @reload="form.reloadFromDisk()" @overwrite="form.keepMine()"
        />
        <div v-if="form.replacing" class="note warn" role="status" data-testid="wf-replacing">
          <Icon name="alert" :size="16" />
          <span>This file cannot be read ({{ form.replacing }}). Saving writes a new watch over it.</span>
        </div>

        <div v-if="form.error" class="note attn" role="alert" data-testid="wf-error"><Icon name="alert" :size="16" /><span>{{ form.error }}</span></div>
      </div>

      <div class="d-body">
        <div v-if="form.loading" class="help" role="status">Reading the workspace…</div>
        <div class="field">
          <label for="wf-name">Name</label>
          <input id="wf-name" v-model="form.name" class="field-input" type="text" autocomplete="off" spellcheck="false" :readonly="form.mode === 'edit' || form.loading" aria-describedby="wf-name-h" data-testid="wf-name" />
          <div id="wf-name-h" class="help">The file name: lower-case letters, digits and underscores. It cannot be changed later.</div>
        </div>
        <div class="field">
          <label for="wf-desc">Description <span class="count">optional</span></label>
          <input id="wf-desc" v-model="form.description" class="field-input" type="text" autocomplete="off" :readonly="form.loading" data-testid="wf-description" />
        </div>

        <fieldset class="wf-group">
          <legend>The question</legend>
          <div class="wf-row" role="radiogroup" aria-label="What to ask">
            <label><input type="radio" name="wf-kind" value="query" :checked="form.kind === 'query'" data-testid="wf-kind-query" @change="form.pickKind('query')" /> A saved query</label>
            <label><input type="radio" name="wf-kind" value="metric" :checked="form.kind === 'metric'" data-testid="wf-kind-metric" @change="form.pickKind('metric')" /> A metric</label>
          </div>
          <div class="field">
            <label for="wf-target">{{ form.kind === 'query' ? 'Saved query' : 'Metric' }}</label>
            <select id="wf-target" class="field-input" :value="form.target" data-testid="wf-target" @change="form.pickTarget(($event.target as HTMLSelectElement).value)">
              <option value="" disabled>Choose…</option>
              <template v-if="form.kind === 'query'">
                <option v-for="q in form.queries" :key="q.id" :value="q.id">{{ q.title || q.id }} ({{ q.id }})</option>
              </template>
              <template v-else>
                <option v-for="m in form.metrics" :key="m.name" :value="m.name">{{ m.title || m.name }}</option>
              </template>
            </select>
            <div v-if="form.kind === 'metric'" class="help">A metric is asked at its total, with no breakdown.</div>
            <div v-else-if="!form.queries.length" class="help">There are no saved queries yet. Save one from the Queries view first.</div>
          </div>
          <div v-for="n in form.paramNames" :key="n" class="field">
            <label :for="`wf-p-${n}`">Parameter <code>${{ n }}</code></label>
            <input
              :id="`wf-p-${n}`" class="field-input" type="text" autocomplete="off" :value="form.params[n] ?? ''" :aria-describedby="`wf-p-${n}-h`"
              :placeholder="form.specOf(n)?.default ?? ''" :data-testid="`wf-param-${n}`" @input="form.setParam(n, ($event.target as HTMLInputElement).value)"
            />
            <div :id="`wf-p-${n}-h`" class="help">{{ paramHint(n) }}. Left empty, the default is used.</div>
          </div>
        </fieldset>

        <fieldset class="wf-group">
          <legend>Fire when</legend>
          <div class="field">
            <label for="wf-cond">Condition</label>
            <select id="wf-cond" v-model="form.cond" class="field-input" data-testid="wf-cond">
              <option v-for="c in CONDITIONS" :key="c.value" :value="c.value">{{ c.label }}</option>
            </select>
          </div>
          <div v-if="form.cond === 'above' || form.cond === 'below' || form.cond === 'outside'" class="wf-row">
            <div class="field">
              <label for="wf-a">{{ form.cond === 'outside' ? 'Low' : 'Number' }}</label>
              <input id="wf-a" v-model="form.a" class="field-input" type="text" inputmode="decimal" autocomplete="off" spellcheck="false" data-testid="wf-a" />
            </div>
            <div v-if="form.cond === 'outside'" class="field">
              <label for="wf-b">High</label>
              <input id="wf-b" v-model="form.b" class="field-input" type="text" inputmode="decimal" autocomplete="off" spellcheck="false" data-testid="wf-b" />
            </div>
          </div>
          <p v-if="form.cond === 'above' || form.cond === 'below' || form.cond === 'outside'" class="help">The question must come back as one row with one numeric column (or a column named <code>value</code>).</p>
        </fieldset>

        <div class="field">
          <span id="wf-prev-l" class="wf-label">The file this writes</span>
          <pre class="sh-mono wf-preview" tabindex="0" aria-labelledby="wf-prev-l" data-testid="wf-preview">{{ form.preview || ' ' }}</pre>
          <div v-if="form.previewError" class="help" data-testid="wf-preview-error">Not ready to save yet: {{ form.previewError }}</div>
        </div>
      </div>

      <div class="d-foot">
        <button class="btn" type="button" :disabled="form.busy" data-testid="wf-cancel" @click="form.close()">Cancel</button>
        <button class="btn btn-primary" type="submit" :disabled="!form.canSave || form.busy || form.loading" data-testid="wf-save">{{ form.busy ? 'Saving…' : 'Save watch' }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.wf-group { border: 0; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sh-space-3); min-width: 0; }
.wf-group legend { font-weight: 600; font-size: 14px; padding: 0; margin-bottom: var(--sh-space-2); }
.wf-row { display: flex; gap: var(--sh-space-4); flex-wrap: wrap; align-items: flex-end; }
.wf-row .field { flex: 1 1 140px; min-width: 0; }
.wf-label { font-size: var(--sh-fs-sm); font-weight: 600; }
.count { font-weight: 400; font-size: 12px; color: var(--sh-graphite); margin-left: 6px; }
.wf-preview { margin: 0; padding: var(--sh-space-3); background: var(--sh-paper-2); border: var(--sh-border-thin); border-radius: var(--sh-radius-sm); white-space: pre-wrap; overflow-wrap: anywhere; font-size: var(--sh-fs-sm); max-height: 200px; overflow: auto; }
.d-alerts { display: flex; flex-direction: column; gap: 12px; padding: 12px 24px 0; flex: none; }
.d-body { max-height: min(64vh, 640px); overflow-y: auto; }
.d-body > * { flex-shrink: 0; }
.help, .note { overflow-wrap: anywhere; }
</style>
