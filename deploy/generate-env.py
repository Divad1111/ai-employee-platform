#!/usr/bin/env python3
"""Generate fresh-install secrets only. Never run this to rotate an existing vault."""
import argparse, os, secrets
from pathlib import Path
from urllib.parse import urlparse
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--domain',required=True)
p.add_argument('--output',type=Path,default=Path(__file__).with_name('.env'))
p.add_argument('--server-image',required=True)
p.add_argument('--admin-image',required=True)
a=p.parse_args()
if urlparse('https://'+a.domain).hostname!=a.domain or any(c in a.domain for c in '/: \n\r$`'):
 p.error('domain must be a plain DNS hostname')
for value in (a.server_image,a.admin_image):
 if any(c in value for c in '\n\r$` '):p.error('invalid image reference')
text=Path(__file__).with_name('.env.example').read_text()
values={'AIE_DOMAIN':a.domain,'AIE_PUBLIC_SERVER_URL':'https://'+a.domain,'AIE_CONTROL_PLANE_IMAGE':a.server_image,'AIE_ADMIN_IMAGE':a.admin_image}
for key in ('POSTGRES_PASSWORD','AIE_SECRET_MASTER_KEY','AIE_BACKUP_MASTER_KEY','AIE_SETUP_TOKEN'):values[key]=secrets.token_hex(32)
lines=[]
for line in text.splitlines():
 key=line.split('=',1)[0]
 lines.append(key+'='+values[key] if key in values else line)
fd=os.open(a.output,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:f.write('\n'.join(lines)+'\n')
print('Created private environment file:',a.output)
