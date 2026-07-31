---
name: release
description: Publica uma nova versão do binário lk — roda os gates determinísticos (make release-preflight), resolve o número semver a partir do SURFACE.txt, cria a tag que dispara o GoReleaser e confirma o cask no tap da Homebrew (make release-verify). Use quando pedirem para publicar, lançar ou subir uma versão do lk, taguear vX.Y.Z, ou quando o brew estiver servindo versão antiga. NÃO cobre expor comandos novos na CLI (ver "Exposing/changing a command" no CLAUDE.md, que acontece antes da release) nem instalação para usuário final (README).
user-invokable: true
---

# Release do lk

Orquestra a publicação de uma nova versão do binário `lk`. As checagens são código
determinístico em `scripts/`; esta skill decide o que exige julgamento (o número da
versão, o momento de pedir aprovação) e chama os gates.

## Related Skills

- Skill `github` (plugin `lk-tools`) — mecânica de git, branch e PR. O código que
  entra na release chega por lá; a release em si não abre PR.
- Skill `lk` (`lk-stack/lk-tools/skills/lk/`) — uso do binário pelo agente.
- Mudança de superfície da CLI (comando novo ou removido) é o processo
  "Exposing/changing a command" do `CLAUDE.md` e acontece **antes** da release.

## Invariantes

1. **Nada publica sem aprovação explícita.** O push da tag é o gatilho da
   publicação. Perguntar "publico a vX.Y.Z?" e esperar resposta.
2. **O cask é gerado.** `Casks/lk.rb` em `linkanalabs/homebrew-tap` sai do
   GoReleaser — nunca editar à mão.
3. **Comando canônico de instalação:** `brew install linkanalabs/tap/lk`, sempre com
   o nome qualificado do tap. `brew upgrade lk` falha em máquina limpa.
4. **FAIL de gate não se contorna.** Reprovou, corrige a causa. Não existe modo
   "pula esse check", e esta skill não improvisa checagem própria em paralelo.

## 1. Gates e versão

```bash
make release-preflight                    # deriva a versão recomendada
make release-preflight VERSION=vX.Y.Z     # valida uma versão específica
```

Sai 1 se qualquer gate falhar; quando tudo passa, imprime `recommended_version=` e
o par de comandos de tag já com a versão resolvida.

A versão vem do `SURFACE.txt`, o golden da árvore de comandos: linha que
desapareceu = comando removido = bump de minor (pré-1.0); linha nova = comando
novo = minor; golden intacto = patch. **Quando o bump for minor por remoção, dizer
quais comandos saíram da superfície antes de seguir** — é quebra de contrato para
quem já usa a CLI, e a pessoa pode querer outro número.

Só a versão, sem rodar a suíte: `./scripts/release-preflight.sh --bump-only`.

## 2. Aprovação e tag

Com o preflight verde, pedir aprovação. Só então:

```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

Usar o par que o preflight imprimiu, não uma versão digitada de memória.

## 3. Acompanhar o workflow

```bash
gh run watch $(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')
```

Três jobs em série: `guard` (semver, tag em commit da `main`, `make cover`) →
`goreleaser` (publica e commita o cask) → `verify` (artefatos, cask, install.sh).
`guard` vermelho significa que **nada foi publicado**.

## 4. Verificar na máquina

O job `verify` já cobre tudo que não depende de Homebrew. Para fechar o ciclo do
usuário final:

```bash
make release-verify VERSION=vX.Y.Z LOCAL=1
```

`LOCAL=1` instala via brew e compara `lk version` — mexe no `lk` da máquina. Sem
ele, nada é instalado no sistema.

## Divisão de responsabilidade dos gates

| Onde | Cobre |
|------|-------|
| `make release-preflight` (local, antes da tag) | commit está na `origin/main`, nada pendente, escopo desde a última tag, bump derivado, golden fresco, CI verde no sha exato, `goreleaser check` se a config mudou, `make test`/`cover`/`lint` |
| job `guard` (servidor, toda tag) | semver, tag em commit da `main`, `make cover` — pega até a tag empurrada sem preflight |
| job `verify` + `make release-verify` | release não-draft, os 7 assets por nome, é a `latest`, cask na versão certa, sha256 do cask contra o `checksums.txt`, instalação via `install.sh` e (com `LOCAL=1`) via brew |

Faltou cobertura? O lugar de acrescentar é o script, não uma checagem manual solta.

## Quando algo falha

Ver [troubleshooting.md](troubleshooting.md) — tag já publicada, cask que não
atualizou, prerelease, release local de contingência.
