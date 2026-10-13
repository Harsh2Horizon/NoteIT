import test from 'node:test';
import assert from 'node:assert/strict';
import {Autosave} from './autosave.mjs';

test('debounces edits into a single versioned save',async()=>{
  const calls=[];const saver=new Autosave({version:3,delay:10000,save:async data=>{calls.push(data);return{version:4};}});
  saver.edit({title:'first'});saver.edit({title:'latest',content:'hello'});await saver.flush();
  assert.deepEqual(calls,[{title:'latest',content:'hello',version:3}]);assert.equal(saver.dirty,false);saver.dispose();
});
test('serializes edits made during an in-flight save using the next version',async()=>{
  let finish;const calls=[];const saved=[];
  const saver=new Autosave({version:1,delay:10000,onSaved:n=>saved.push(n.version),save:async data=>{calls.push(data);if(calls.length===1)await new Promise(r=>finish=r);return {version:data.version+1};}});
  saver.edit({content:'first'});const flushing=saver.flush();saver.edit({content:'second'});finish();await flushing;
  assert.deepEqual(calls,[{content:'first',version:1},{content:'second',version:2}]);assert.deepEqual(saved,[2,3]);assert.equal(saver.dirty,false);saver.dispose();
});
test('conflicts preserve the newest local edits and block overwriting',async()=>{
  let reject;let calls=0;
  const saver=new Autosave({version:1,delay:10000,save:async()=>{calls++;await new Promise((_,r)=>reject=r);}});
  saver.edit({content:'old local'});const flushing=saver.flush();saver.edit({content:'new local'});
  const error=new Error('conflict');error.status=409;reject(error);
  await assert.rejects(flushing);assert.equal(saver.pending.content,'new local');assert.equal(saver.blocked,true);
  await assert.rejects(saver.flush());assert.equal(calls,1);saver.dispose();
});
test('a failed request retains the draft and can recover',async()=>{
  let fail=true;
  const saver=new Autosave({version:5,delay:10000,save:async data=>{if(fail)throw new Error('offline');return {version:data.version+1};}});
  saver.edit({title:'safe'});await assert.rejects(saver.flush());assert.equal(saver.pending.title,'safe');assert.equal(saver.version,5);
  fail=false;await saver.flush();assert.equal(saver.version,6);assert.equal(saver.dirty,false);saver.dispose();
});
