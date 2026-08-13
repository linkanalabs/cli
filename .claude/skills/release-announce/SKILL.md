---
name: release-announce
description: >
  Avisa o time no Slack #general-avisos que saiu uma versão nova do `lk`.
  Dispara sozinha sempre que o contexto for "versão lançada": tag `vX.Y.Z`
  empurrada, workflow de release verde, cask atualizado na tap, ou o Djalma
  dizendo "avisa da versão nova", "comunica o release", "manda no avisos".
  SEMPRE mostra o preview da mensagem e pede aprovação antes de enviar —
  nunca posta direto.
---

# Aviso de versão nova do `lk` no #general-avisos

Canal: **#general-avisos** (`C016CJQSR4H`). Público, empresa toda, maioria
non-tech.

## 1. Antes de escrever, confirme que a versão existe de verdade

Nunca anunciar uma versão que não dá pra instalar. Os três checks (o segundo já
falhou silenciosamente antes — release sai, tap não atualiza, e o brew serve a
versão velha):

```sh
gh release view vX.Y.Z --repo linkanalabs/cli --json tagName,assets
gh api repos/linkanalabs/homebrew-tap/commits --jq '.[0].commit.message'   # deve citar vX.Y.Z
gh release download vX.Y.Z --repo linkanalabs/cli --pattern "lk_X.Y.Z_linux_amd64.tar.gz" \
  && tar xzf lk_X.Y.Z_linux_amd64.tar.gz && ./lk version                   # deve imprimir a versão
```

Algum falhou → não anuncia. Reporta o furo primeiro.

## 2. Monte a mensagem neste padrão

````
**`lk` vX.Y.Z no ar** 🚀
Destaque: <uma linha, non-tech, sobre o que a pessoa passa a conseguir fazer>

Atualizar:
```
brew install linkanalabs/tap/lk
```
No Linux, o comando de instalação está no [README](https://github.com/linkanalabs/cli#instalação).

O que dá pra fazer agora:
```
lk <comando novo>
```
<uma frase dizendo o que esse comando resolve, na língua de quem usa>
````

O bloco "O que dá pra fazer agora" é opcional numa release só de correção, e
obrigatório quando entram comandos: o time quer ver o que passou a ser possível,
não a lista de PRs.

Regras do texto:

- **PT-BR.** Quem lê é CS, Vendas, Financeiro — não time técnico.
- **Uma linha de destaque só**, em termos de resultado ("montar um formulário de
  fornecedor inteiro pela linha de comando"), não em nome de comando
  (`settings performance-ask calibrate`) nem em nome de PR/issue.
- **Mostre o comando, esconda o payload.** Um comando por bloco, só até o
  subcomando — `lk settings supplier-field field create`. **Nunca** flag com JSON
  (`--classification '{"<id>":{"correct":true}}'`), nunca nome de campo interno
  (`display_name`, `setting_grouper_id`, `field_type`), nunca id de exemplo. Quem
  precisa dos parâmetros usa o `--help`, e a última linha da mensagem aponta pra lá.
- **Uma frase por comando, no efeito e não no mecanismo.** "Define quanto cada
  pergunta vale e a nota mínima para o fornecedor passar" — não "seta `weights` e
  `grade_threshold` no `setting_document_review`". Sem código de status (404, 422),
  sem nome de policy, sem nome de tabela ou de classe.
- **Até 6 comandos por mensagem.** Passando disso, mostre os que mudam o dia a dia
  e feche com "são N comandos novos; `lk <grupo> --help` mostra todos".
- **Write irreversível ou em massa ganha aviso**, em português claro e na mesma
  frase do comando: "atinge todos os fornecedores aprovados e monitorados de uma
  vez, e não tem como desfazer". Quem lê o anúncio decide se vai testar em
  produção — a ressalva não pode ficar só no `--help`.
- `brew install linkanalabs/tap/lk` é o comando canônico — instala **e** atualiza,
  funciona em qualquer estado. Nunca sugerir `brew upgrade lk` (falha em máquina
  limpa: é cask em tap dedicada, não formula do core).
- **NÃO colocar o comando `curl ... | sh` no corpo.** O WAF na frente do conector
  do Slack rejeita o envio (bloqueio Cloudflare, não erro do Slack) — o padrão
  piped-to-shell é assinatura de ataque. Linkar a seção Instalação do README
  resolve. Se pedirem o comando literal no canal, manda como snippet/arquivo ou
  deixa a pessoa colar numa thread.
- Emoji: só o 🚀 do título. Sem atribuição a IA em lugar nenhum.

## 3. Pergunte antes de enviar (obrigatório)

Use `AskUserQuestion` com o texto final no `preview` da opção. Opções úteis:
"Pode mandar", "Sem emoji", "Trocar o destaque". Nada vai pro canal sem o
"pode mandar" explícito — vale para toda mensagem externa, e num canal da
empresa toda o custo de errar é alto.

Aprovado → `slack_send_message` no `C016CJQSR4H` e devolve o link da mensagem.
Se o envio for barrado, o suspeito nº 1 é o corpo (ver a regra do `curl` acima),
não o canal: um read (`slack_search_channels`) confirma se o conector está de pé
antes de mexer no texto.

## 4. Referência — mensagem da v0.12.0 (13/08/2026)

O formato aprovado, com comandos e sem payload. Repare que nenhum bloco tem flag:

> **`lk` v0.12.0 no ar** 🚀
> Destaque: dá pra montar um formulário de fornecedor inteiro pela linha de comando — perguntas, opções de resposta, quais respostas são certas e quanto cada uma vale.
>
> Atualizar:
> ```
> brew install linkanalabs/tap/lk
> ```
> No Linux, o comando de instalação está no [README](https://github.com/linkanalabs/cli#instalação).
>
> O que dá pra fazer agora:
> ```
> lk settings supplier-field create
> ```
> Cria um formulário novo. Antes, só pela tela.
> ```
> lk settings supplier-field field create
> ```
> Adiciona uma pergunta, com as opções de resposta que o fornecedor vai escolher.
> ```
> lk settings supplier-field answer-key set
> ```
> Define quanto cada pergunta vale e a nota mínima para o fornecedor passar.
> ```
> lk settings supplier-field request-resend
> ```
> Pede que os fornecedores respondam o formulário de novo. Atinge todos os aprovados e monitorados de uma vez, e não tem como desfazer.
>
> São 12 comandos novos. `lk settings supplier-field --help` mostra todos, com o que cada um precisa.

Antes dela, a v0.7.0 (31/07/2026) tinha só destaque e instalação, sem bloco de
comandos — foi o que o time pediu para mudar. Ela também é o registro de que a
linha do `curl` foi bloqueada duas vezes pelo WAF antes de sair.
