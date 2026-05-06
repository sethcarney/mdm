# mdm skills find

Search the skills registry and install interactively.

## Usage

```
mdm skills find [query]
```

Queries the [skills.sh](https://skills.sh) registry and shows a list of matching skills. Select one or more to install immediately. If no query is provided, you are prompted to enter one.

Aliases: `search`, `f`, `s`

## Flow

1. Enter a search query (or pass it as an argument).
2. A spinner shows while results are fetched.
3. A multiselect list shows matching skills with their descriptions and star counts.
4. Selecting skills runs `mdm skills add` for each one, letting you choose scope and agents.

```
Search skills: typescript

  ◉ typescript-best-practices   Enforce strict TypeScript patterns ★142
  ○ typescript-react             React + TypeScript guidelines ★67
  ○ ts-testing                   Testing strategies for TypeScript ★23
```

## Examples

```bash
# Search with a query
mdm skills find typescript

# Open the search prompt interactively
mdm skills find

# Using the alias
mdm skills search git
```

## Registry

Skills are sourced from the public [skills.sh](https://skills.sh) registry. To list your own skill package in the registry, see the publishing guide at skills.sh.
