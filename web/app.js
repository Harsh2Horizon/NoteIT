import {renderMarkdown} from './markdown.mjs';
import {Autosave} from './autosave.mjs';

const $ = id => document.getElementById(id);
const state = {user:null, note:null, saver:null, folder:'active', tag:'', offset:0, total:0, notes:[], mode:'login', listRequest:0, preview:false};
const show = (id, visible=true) => { $(id).hidden=!visible; };
const date = value => new Date(value).toLocaleDateString(undefined,{month:'short',day:'numeric'});
let navigating=false;
const disableEditor = disabled => {for(const id of ['note-title','note-content','note-tags'])$(id).disabled=disabled;};
let noticeTimer;
function notice(message, error=false) {
  clearTimeout(noticeTimer); $('notice').textContent=message; $('notice').classList.toggle('error',error); show('notice');
  if (!error) noticeTimer=setTimeout(()=>show('notice',false),9000);
}
async function api(path, {method='GET', body}={}) {
  let response;
  try { response=await fetch(path,{method,credentials:'same-origin',headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(15000)}); }
  catch { throw new Error('Connection unavailable. Your unsaved text stays in this browser; reconnect to continue.'); }
  const data=await response.json().catch(()=>({error:'The service is temporarily unavailable.'}));
  if (!response.ok) { const err=new Error(data.error || 'Request failed'); err.status=response.status; err.data=data; throw err; }
  return data;
}
function action(fn) { return async event => { try { await fn(event); } catch(err) { notice(err.message,true); } }; }
function view(name) { for(const id of ['auth-view','workspace','shared-view']) show(id,id===name); }
function account() { $('account-email').textContent=state.user?.email || ''; show('signout',!!state.user); }
function authMode(mode) {
  state.mode=mode;
  $('login-tab').classList.toggle('active',mode==='login'); $('register-tab').classList.toggle('active',mode==='register');
  $('auth-title').textContent=mode==='login'?'Welcome back.':'A fresh page awaits.';
  $('auth-description').textContent=mode==='login'?'Your thoughts are right where you left them.':'Create your account, then verify your email.';
  $('auth-submit').textContent=mode==='login'?'Sign in':'Create account';
  $('password').autocomplete=mode==='login'?'current-password':'new-password';
  $('password').minLength=mode==='login'?1:12; show('password-help',mode==='register');
}
$('login-tab').onclick=()=>authMode('login'); $('register-tab').onclick=()=>authMode('register');
$('auth-form').onsubmit=action(async event=>{
  event.preventDefault(); $('auth-submit').disabled=true;
  try {
    const body={email:$('email').value,password:$('password').value};
    if (state.mode==='register') { const result=await api('/api/auth/register',{method:'POST',body}); notice(result.message); authMode('login'); $('password').value=''; }
    else {
      state.user=await api('/api/auth/login',{method:'POST',body}); account(); $('password').value='';
      const back=sessionStorage.getItem('noteit-return'); sessionStorage.removeItem('noteit-return');
      if (back && /^\/s\/[a-f0-9]{64}$/.test(back)) { location.assign(back); return; }
      show('notice',false); await enterWorkspace();
    }
  } finally { $('auth-submit').disabled=false; }
});
$('resend').onclick=action(async()=>{
  if (!$('email').reportValidity()) return;
  const result=await api('/api/auth/resend-verification',{method:'POST',body:{email:$('email').value}}); notice(result.message);
});
$('signout').onclick=action(async()=>{
  await flush(); await api('/api/auth/logout',{method:'POST',body:{}});
  state.saver?.dispose(); state.saver=null; state.user=null; state.note=null; state.notes=[];
  $('note-content').value=''; $('note-title').value=''; $('preview').replaceChildren(); $('note-list').replaceChildren(); account();
  location.assign('/');
});

async function enterWorkspace() { view('workspace'); await Promise.all([loadNotes(),loadTags()]); }
async function loadNotes() {
  const request=++state.listRequest;
  const query=new URLSearchParams({state:state.folder==='pinned'?'active':state.folder,q:$('search').value,tag:state.tag,limit:'30',offset:String(state.offset)});
  if (state.folder==='pinned') query.set('pinned','true');
  const result=await api('/api/notes?'+query);
  if(request!==state.listRequest) return;
  state.notes=result.notes; state.total=result.total;
  if (state.offset>0 && state.offset>=state.total) { state.offset=Math.max(0,state.offset-30); return loadNotes(); }
  renderList();
}
function renderList() {
  $('note-count').textContent=String(state.total);
  $('folder-title').textContent={active:'All notes',pinned:'Pinned',archived:'Archive',trash:'Trash'}[state.folder];
  $('note-list').replaceChildren();
  if (!state.notes.length) { const p=document.createElement('p'); p.className='list-empty'; p.textContent=$('search').value || state.tag?'No notes match this search.':'Nothing here yet. A new note is a good place to start.'; $('note-list').append(p); }
  for(const note of state.notes) {
    const card=document.createElement('button'); card.className='note-card'; card.classList.toggle('selected',note.id===state.note?.id);
    const title=document.createElement('h3'); title.textContent=(note.pinned?'• ':'')+(note.title || 'Untitled');
    const excerpt=document.createElement('p'); excerpt.textContent=note.excerpt || 'An empty page, full of possibility.';
    const footer=document.createElement('footer'); const time=document.createElement('span'); time.textContent=date(note.updated_at);
    const tag=document.createElement('span'); tag.textContent=note.tags.slice(0,2).map(t=>'#'+t).join(' · ');
    footer.append(time,tag); card.append(title,excerpt,footer); card.onclick=action(()=>openNote(note.id)); $('note-list').append(card);
  }
  $('previous').disabled=state.offset===0; $('next').disabled=state.offset+30>=state.total;
  $('page-label').textContent=state.total?`${state.offset+1}–${Math.min(state.offset+30,state.total)}`:'0 notes';
}
async function loadTags() {
  const result=await api('/api/tags'); $('tag-list').replaceChildren();
  for(const tag of result.tags) {
    const button=document.createElement('button'); button.textContent='# '+tag.name; button.classList.toggle('active',state.tag===tag.name);
    const count=document.createElement('span'); count.textContent=String(tag.count); button.append(count);
    button.onclick=action(async()=>{ await flush(); state.tag=state.tag===tag.name?'':tag.name; state.offset=0; await Promise.all([loadNotes(),loadTags()]); });
    $('tag-list').append(button);
  }
  show('clear-tag',!!state.tag);
}
$('clear-tag').onclick=action(async()=>{await flush();state.tag='';state.offset=0;await Promise.all([loadNotes(),loadTags()]);});
for(const button of $('filters').querySelectorAll('button')) button.onclick=action(async()=>{
  await flush(); state.folder=button.dataset.state; state.offset=0;
  for(const item of $('filters').querySelectorAll('button')) item.classList.toggle('active',item===button);
  await loadNotes();
});
let searchTimer;
$('search').oninput=()=>{ clearTimeout(searchTimer); searchTimer=setTimeout(()=>{state.offset=0;loadNotes().catch(err=>notice(err.message,true));},250); };
$('previous').onclick=action(async()=>{state.offset=Math.max(0,state.offset-30);await loadNotes();});
$('next').onclick=action(async()=>{state.offset+=30;await loadNotes();});

function draftKey(id=state.note?.id) { return `noteit-draft:${state.user.id}:${id}`; }
function editorData() { return {title:$('note-title').value,content:$('note-content').value,tags:$('note-tags').value.split(',').map(t=>t.trim()).filter(Boolean)}; }
function persistDraft() {
  if(!state.note || !state.saver) return;
  try { localStorage.setItem(draftKey(),JSON.stringify({version:state.saver.version,...editorData()})); }
  catch { notice('Browser draft storage is full or unavailable. Keep this tab open until all changes are saved.',true); }
}
function clearDraft() { try { localStorage.removeItem(draftKey()); } catch {} }
function readDraft() { try { const d=JSON.parse(localStorage.getItem(draftKey())); return d && typeof d.title==='string' && typeof d.content==='string' && Array.isArray(d.tags) && d.tags.every(t=>typeof t==='string')?d:null; } catch { return null; } }
async function flush() { if(state.saver) await state.saver.flush(); }
function bindSaver(note) {
  state.saver?.dispose();
  const id=note.id;
  state.saver=new Autosave({version:note.version,
    save:body=>api('/api/notes/'+id,{method:'PATCH',body}),
    onSaved:saved=>{
      state.note=saved;
      if (state.saver.pending) persistDraft(); else clearDraft();
      updateMeta();
      loadNotes().catch(()=>{}); loadTags().catch(()=>{});
    },
    onStatus:(status,err)=>{
      $('save-status').textContent={pending:'Unsaved changes',saving:'Saving…',saved:'All changes saved',error:'Not saved — '+(err?.message || 'try again'),conflict:'Conflict — your draft is safe'}[status];
      if(status==='conflict') {show('conflict');persistDraft();}
      if(status==='error') persistDraft();
    }
  });
}
async function openNote(id) {
  if(navigating || mutating)return;
  navigating=true;disableEditor(true);
  try{
  await flush();
  const note=await api('/api/notes/'+id); state.note=note; bindSaver(note); renderEditor(); renderList();
  const draft=readDraft();
  if(draft && !note.deleted_at) {
    $('note-title').value=draft.title; $('note-content').value=draft.content; $('note-tags').value=draft.tags.join(', ');
    if(draft.version!==note.version) state.saver.blocked=true;
    state.saver.edit(editorData()); updateMeta();
    notice(draft.version===note.version?'Recovered your local draft. Autosave will resume.':'Recovered a local draft, but this note changed elsewhere. Review the conflict before continuing.',draft.version!==note.version);
  }
  }finally{navigating=false;disableEditor(false);}
}
function renderEditor() {
  const n=state.note; show('empty-editor',!n); show('editor',!!n); show('conflict',false); if(!n)return;
  $('note-title').value=n.title; $('note-content').value=n.content; $('note-tags').value=n.tags.join(', ');
  $('save-status').textContent='All changes saved';
  $('pin-note').textContent=n.pinned?'Unpin':'Pin'; $('archive-note').textContent=n.archived?'Unarchive':'Archive';
  for(const id of ['pin-note','archive-note','share-note','delete-note']) show(id,!n.deleted_at);
  show('restore-note',!!n.deleted_at); $('share-note').disabled=n.archived;
  for(const id of ['note-title','note-content','note-tags']) $(id).readOnly=!!n.deleted_at;
  updateMeta(); previewMode(false);
}
function updateMeta() {
  if(!state.note)return;
  $('note-updated').textContent='Edited '+date(state.note.updated_at);
  const words=$('note-content').value.trim().split(/\s+/).filter(Boolean).length;
  $('word-count').textContent=`${words} ${words===1?'word':'words'}`;
  if(state.preview)$('preview').innerHTML=renderMarkdown($('note-content').value);
}
for(const id of ['note-title','note-content','note-tags']) $(id).oninput=()=>{
  if(!state.saver || state.note.deleted_at)return;
  state.saver.edit(editorData());persistDraft();updateMeta();
};
function previewMode(enabled) {
  state.preview=enabled; show('preview',enabled); show('note-content',!enabled);
  $('write-tab').classList.toggle('active',!enabled); $('preview-tab').classList.toggle('active',enabled);
  if(enabled)$('preview').innerHTML=renderMarkdown($('note-content').value);
}
$('write-tab').onclick=()=>previewMode(false); $('preview-tab').onclick=()=>previewMode(true);
$('new-note').onclick=action(async()=>{
  if(navigating || mutating)return;
  navigating=true;disableEditor(true);
  $('new-note').disabled=true;
  try {
    await flush(); const note=await api('/api/notes',{method:'POST',body:{title:'',content:'',tags:[]}});
    state.note=note;bindSaver(note);renderEditor();state.folder='active';state.tag='';state.offset=0;$('search').value='';
    for(const button of $('filters').querySelectorAll('button'))button.classList.toggle('active',button.dataset.state==='active');
    await Promise.all([loadNotes(),loadTags()]);$('note-title').focus();
  }finally{$('new-note').disabled=false;navigating=false;disableEditor(false);}
});
let mutating=false;
async function mutate(suffix,method,extra={}) {
  if(mutating || navigating)return;
  mutating=true;
  for(const id of ['note-title','note-content','note-tags'])$(id).disabled=true;
  try {
    await flush();
    const note=await api('/api/notes/'+state.note.id+suffix,{method,body:{version:state.saver.version,...extra}});
    state.note=note;bindSaver(note);renderEditor();await Promise.all([loadNotes(),loadTags()]);
    return note;
  }catch(err){
    if(err.status===409){state.saver.blocked=true;show('conflict');persistDraft();$('save-status').textContent='Note changed elsewhere — reload required';}
    throw err;
  }finally{mutating=false;for(const id of ['note-title','note-content','note-tags'])$(id).disabled=false;}
}
$('pin-note').onclick=action(()=>mutate('','PATCH',{pinned:!state.note.pinned}));
$('archive-note').onclick=action(async()=>{const archiving=!state.note.archived;await mutate('','PATCH',{archived:archiving});notice(archiving?'Note archived. Any published link has been revoked.':'Note moved back to your notebook.');});
$('delete-note').onclick=action(async()=>{await mutate('','DELETE');notice('Note moved to Trash. You can restore it anytime. Its published link has been revoked.');});
$('restore-note').onclick=action(async()=>{await mutate('/restore','POST');notice('Note restored to your notebook.');});
$('download-draft').onclick=()=>{
  const body=`# ${$('note-title').value || 'Untitled'}\n\n${$('note-content').value}\n\nTags: ${$('note-tags').value}\n`;
  const url=URL.createObjectURL(new Blob([body],{type:'text/markdown;charset=utf-8'}));
  const a=document.createElement('a');a.href=url;a.download='noteit-local-draft.md';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
};
$('reload-note').onclick=action(async()=>{
  const note=await api('/api/notes/'+state.note.id);state.saver.dispose();clearDraft();state.note=note;bindSaver(note);renderEditor();show('notice',false);
});
window.addEventListener('beforeunload',event=>{if(state.saver?.dirty || state.saver?.blocked){persistDraft();event.preventDefault();event.returnValue='';}});
document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='hidden' && state.saver?.dirty){persistDraft();flush().catch(()=>{});}});
window.addEventListener('online',()=>flush().catch(()=>{}));

function publicationFields() {
  const n=state.note;$('visibility').value=n.visibility;$('allowed-emails').value=n.allowed_emails.join(', ');
  show('allowed-group',n.visibility==='restricted');show('published-link',!!n.share_url);show('unpublish',!!n.share_url);
  $('share-url').value=n.share_url || '';$('publish-error').textContent='';
}
$('share-note').onclick=action(async()=>{await flush();publicationFields();$('publish-dialog').showModal();});
$('close-publish').onclick=()=>$('publish-dialog').close();
$('visibility').onchange=()=>show('allowed-group',$('visibility').value==='restricted');
$('publish-form').onsubmit=async event=>{
  event.preventDefault();const submit=event.submitter;submit.disabled=true;
  try {
    await mutate('/publication','PUT',{visibility:$('visibility').value,allowed_emails:$('allowed-emails').value.split(/[\n,]/).map(e=>e.trim()).filter(Boolean)});
    publicationFields();notice('Publication access updated.');
  }catch(err){$('publish-error').textContent=err.message;}finally{submit.disabled=false;}
};
$('unpublish').onclick=action(async()=>{await mutate('/publication','DELETE');publicationFields();notice('Link revoked. Publishing again will create a new link.');});
$('copy-link').onclick=action(async()=>{
  try{await navigator.clipboard.writeText($('share-url').value);notice('Link copied.');}
  catch{$('share-url').select();notice('Select and copy the link from this field.');}
});

async function shared(token) {
  view('shared-view');
  try {const note=await api('/api/published/'+token);$('shared-title').textContent=note.title || 'Untitled';$('shared-date').textContent='Updated '+date(note.updated_at);$('shared-content').innerHTML=renderMarkdown(note.content);show('shared-error',false);}
  catch(err){$('shared-title').textContent='This note is unavailable.';$('shared-error-text').textContent=err.message;show('shared-error');show('shared-signin',err.status===401);}
}
$('shared-signin').onclick=()=>{sessionStorage.setItem('noteit-return',location.pathname);location.assign('/');};
async function boot() {
  let verification=new URLSearchParams(location.hash.slice(1)).get('verify');
  if(verification){history.replaceState(null,'','/');view('auth-view');show('verify-panel');show('login-panel',false);
    $('verify-button').onclick=action(async()=>{
      $('verify-button').disabled=true;
      try{const result=await api('/api/auth/verify',{method:'POST',body:{token:verification}});verification=null;notice(result.message);show('verify-panel',false);show('login-panel');authMode('login');}
      catch(err){show('verify-panel',false);show('login-panel');throw err;}
    });return;
  }
  try{state.user=await api('/api/auth/me');}catch(err){if(err.status!==401)notice(err.message,true);}
  account();
  const match=/^\/s\/([a-f0-9]{64})$/.exec(location.pathname);
  if(match){await shared(match[1]);return;}
  if(state.user)await enterWorkspace();else{view('auth-view');authMode('login');}
}
boot().catch(err=>notice(err.message,true));
