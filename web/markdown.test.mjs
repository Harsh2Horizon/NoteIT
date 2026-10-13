import test from 'node:test';
import assert from 'node:assert/strict';
import {renderMarkdown} from './markdown.mjs';

test('renders text Markdown blocks and inline features',()=>{
  const html=renderMarkdown('# Heading\n\nA **bold** and *italic* paragraph with `code`.\n\n- item\n- [x] done\n\n1. first\n2. next\n\n> quote\n\n```js\nconst x = 1 < 2;\n```\n\n---');
  for(const part of ['<h1>Heading</h1>','<strong>bold</strong>','<em>italic</em>','<code>code</code>','<ul>','<ol>','disabled checked','<blockquote>quote</blockquote>','1 &lt; 2','<hr>'])assert.ok(html.includes(part),part);
});
test('never executes HTML or embeds graphics, audio, or video',()=>{
  const html=renderMarkdown('<script>alert(1)</script>\n<img src=x onerror=alert(1)>\n<video src="remote"></video>\n![alt](https://tracker.example/image.png)');
  assert.ok(!/<script|<img|<video|<audio|<iframe/.test(html));
  assert.ok(html.includes('&lt;script&gt;'));assert.ok(!html.includes('tracker.example'));assert.ok(html.includes('alt'));
});
test('links allow only safe schemes and escape attributes',()=>{
  const html=renderMarkdown('[unsafe](javascript:alert) [data](data:text/html,hello) [safe](https://example.com/"onclick="x)');
  assert.ok(!html.includes('href="javascript:'));assert.ok(!html.includes('href="data:'));assert.ok(!html.includes('"onclick="'));assert.ok(html.includes('rel="noopener noreferrer"'));
});
test('unclosed code is escaped and parser handles large text',()=>{
  assert.ok(renderMarkdown('```\n<script>x</script>').includes('&lt;script&gt;'));
  assert.ok(renderMarkdown('['.repeat(200000)).length>=200000);
  assert.ok(renderMarkdown('![a]('.repeat(40000)).length>=200000);
});
