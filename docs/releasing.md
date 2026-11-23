# Publier une version de `ballast`

Une version est un tag git. Tout le reste — compilation, sommes, publication — est fait par
`.github/workflows/release.yml`, qui n'appelle que `ops/release/build.sh` et
`ops/release/publish.sh`. Ces deux scripts sont testés sur une machine (`go test ./ops/release`),
pas en poussant un tag pour voir.

## Avant de pousser un tag

1. Être sur `main`, à jour, avec `make test` et `make lint` verts. Le workflow refuse un tag
   dont le commit n'est pas un ancêtre de `origin/main`.
2. Essayer la construction en local, sans rien publier :

        make release-cli VERSION=v0.1.0-rc.1

   Elle écrit les six binaires et `checksums.txt` dans `dist/` (non suivi), compile chaque
   binaire deux fois et échoue si les deux diffèrent. Les sommes se vérifient avec
   `cd dist && shasum -a 256 -c checksums.txt` (ou `sha256sum -c`).

## Publier

Toujours une pré-release d'abord : un tag avec un trait d'union (`-rc.1`) est publié en
pré-release, et se supprime sans conséquence.

    git tag -a v0.1.0-rc.1 -m "ballast v0.1.0-rc.1"
    git push origin v0.1.0-rc.1

Regarder l'exécution dans l'onglet Actions, puis télécharger un binaire de la page des
releases et le vérifier avec les commandes des notes. Quand c'est bon :

    git tag -a v0.1.0 -m "ballast v0.1.0"
    git push origin v0.1.0

Le tag doit être du SemVer strict : `vX.Y.Z` ou `vX.Y.Z-pre-release`, sans métadonnée `+`,
au plus 64 caractères. Le workflow le vérifie par le même script que la construction.

## Ce que fait le workflow

Dans cet ordre, dans un seul job, avec `contents: write` et rien d'autre :

1. le tag est une version ;
2. son commit est sur `main` ;
3. `go test ./internal/cli/... ./internal/domain/...` ;
4. `ops/release/build.sh` : six binaires et `checksums.txt` ;
5. `ops/release/publish.sh` : la release est créée en **brouillon** avec ses sept fichiers,
   les fichiers sont comptés, puis le brouillon est publié. Personne ne lit une release à
   laquelle il manque un binaire.

Le jeton du workflow n'est donné qu'à la dernière étape. Aucun secret du dépôt n'est utilisé.

## Quand ça échoue

- **Avant la publication** (tag mal formé, hors de `main`, tests, construction non
  reproductible) : rien n'est créé. Corriger, supprimer le tag, le recréer :

        git push --delete origin v0.1.0-rc.1
        git tag -d v0.1.0-rc.1

- **Pendant la publication** : le script supprime le brouillon qu'il vient de créer, et
  seulement lui. Il ne supprime jamais une release publiée, ni le tag.
- **Une release existe déjà pour ce tag** : le script refuse et n'y touche pas. Une release
  publiée ne se remplace pas ; on publie la version suivante.

## Un réglage qui n'est pas dans le dépôt

N'importe qui avec le droit d'écriture sur le dépôt peut pousser un tag, donc déclencher
une publication. Le workflow exige que le commit soit sur `main`, mais la protection de tags
est un réglage du dépôt, à faire une fois dans Settings → Rules : une règle qui ne permet
qu'au mainteneur de créer les tags `v*`.

## Ce que les sommes ne disent pas

`checksums.txt` est publié dans la même release que les binaires : il protège d'un
téléchargement abîmé, pas d'une release compromise, et ce n'est pas une signature (la signature
est hors périmètre). Les constructions étant reproductibles, quiconque peut reconstruire le
tag et comparer ; `docs/cli.md` le dit aux utilisateurs.

## Publier l'Action GitHub

L'Action est dans un autre dépôt, `myballast/ballast-action`. Son contenu est le dossier
`ops/ballast-action/` de celui-ci, **sans les fichiers `_test.go`** : les tests restent ici,
parce qu'ils vérifient l'Action contre le code de `ballast` et contre `ops/release/build.sh`
(`go test ./ops/ballast-action`, compris dans `make test`).

Une version de l'Action vient après celle de `ballast` qu'elle installe, jamais avant : le
fichier `VERSION` nomme une release qui doit exister et être publiée.

1. Publier la release de `ballast` (section précédente), puis écrire sa version dans
   `ops/ballast-action/VERSION`. Une seule ligne, `vX.Y.Z` ou `vX.Y.Z-rc.N`.
2. `make test` : le test refuse un `VERSION` qui n'est pas une version, et accorde l'Action
   avec la définition d'une version de `build.sh`.
3. Copier le dossier dans le dépôt de l'Action, sans les tests, y compris `.github/` :

        rsync -a --delete --exclude '*_test.go' --exclude '.git' ops/ballast-action/ ../ballast-action/

4. Dans le dépôt de l'Action, le workflow `install` télécharge la vraie release sur six
   systèmes et vérifie la version : c'est le seul essai réel du téléchargement. Il doit être
   vert avant d'aller plus loin.
5. Commiter, puis tagger : un tag exact, et le tag mobile de la version majeure.

        git tag -a v1.0.0 -m "ballast-action v1.0.0"
        git tag -f v1 v1.0.0
        git push origin v1.0.0
        git push -f origin v1

   `@v1` pointe ainsi toujours sur la dernière version `1.x.y`, et chaque version de l'Action
   n'utilise qu'une commande connue (celle de `VERSION`), jamais « la dernière ». La
   documentation recommande aussi d'épingler par le hash du commit. Le tag exact `v1.0.0` ne
   se déplace jamais.

Une pré-release de `ballast` ne devrait pas être la version d'un tag `v1` : tant que
`VERSION` vaut `-rc.N`, ne pas pousser `v1`.

Côté dépôt de l'Action, comme pour `ballast`, une règle de protection des tags (`v*`) ne doit
laisser que le mainteneur les créer ou les déplacer : `@v1` est le tag qu'un attaquant
voudrait déplacer.

## Publier le modèle GitLab

Le modèle est un seul fichier, `ops/gitlab/ballast.gitlab-ci.yml`, qu'un projet inclut par
l'adresse brute du dépôt `myballast/ballast` **à un tag** : il n'y a rien d'autre à copier ni
à publier.

1. `ops/gitlab/VERSION` doit être celle de `ops/ballast-action/VERSION` : les deux partent
   ensemble (un test le tient), et c'est la version du binaire que le modèle télécharge.
2. Le fichier est **généré** : après un changement d'un script ou de `VERSION`,
   `ops/gitlab/generate.sh > ops/gitlab/ballast.gitlab-ci.yml`. Un test refuse un fichier qui
   n'est pas ce que le générateur écrit.
3. L'image du job est épinglée par empreinte dans `generate.sh` ; la changer, c'est changer
   l'empreinte, et rejouer l'essai ci-dessous.
4. Pousser le tag `vX.Y.Z` du dépôt (c'est le même tag que celui de la release des
   binaires) : le fichier au tag est ce que les projets incluent.

**Non exercé sur un vrai GitLab** : le job a été joué dans l'image Alpine du modèle (même
empreinte, `sh` de busybox, le vrai binaire `ballast` construit pour Linux, un faux serveur
Ballast sur la boucle locale) : un seuil tenu sort 0, un seuil dépassé sort 1. À vérifier au
premier tag, sur un vrai projet : le runner, le masquage de la variable, `include: remote`, et
l'arrêt d'un job annulé.
