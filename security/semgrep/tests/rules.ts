// Static Semgrep fixture only. Never import or execute this file.
function review(source: string, node: HTMLElement, text: string) {
  // ruleid: forcad-js-dynamic-code
  Function(source);
  // ok: forcad-js-dynamic-code
  JSON.parse(source);

  // ruleid: forcad-js-document-write
  document.write(text);
  // ruleid: forcad-js-inner-html
  node.innerHTML += text;
  // ok: forcad-js-inner-html
  node.textContent = text;

  // ruleid: forcad-js-disabled-tls
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
}
