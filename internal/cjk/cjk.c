/*
** cjk: an SQLite FTS5 tokenizer for Chinese / Japanese / Korean text.
**
** CJK runs become overlapping character bigrams (the Lucene CJKAnalyzer /
** Elasticsearch "cjk" approach); everything else is handed to unicode61, so
** Latin text keeps case folding, diacritic removal and the usual options.
** CJK orthographic variants and half-width kana are folded; full-width ASCII
** is folded too. Existing indexes must be rebuilt after upgrading.
**
**   CREATE VIRTUAL TABLE t USING fts5(body, tokenize='cjk');
**   CREATE VIRTUAL TABLE t USING fts5(body, tokenize='cjk unigram 1');
**   CREATE VIRTUAL TABLE t USING fts5(body, tokenize='cjk unigram 1 remove_diacritics 2');
** unigram 1 additionally indexes single characters, without changing phrase
** adjacency; unigram 0 (the default) keeps the original bigram behavior.
**
** A query bareword that tokenizes into several bigrams is matched by FTS5 as a
** phrase, so  MATCH '魚池鄉'  requires 魚池 and 池鄉 to be adjacent, i.e. it is a
** substring match. Space-separated words are ANDed as usual.
*/
#include "sqlite3ext.h"
SQLITE_EXTENSION_INIT1
#include <string.h>
#include <stdlib.h>

typedef struct CjkTokenizer {
  fts5_tokenizer parent;   /* unicode61 */
  Fts5Tokenizer *pParent;
  int unigram;
} CjkTokenizer;

typedef int (*xTokenFn)(void*, int, const char*, int, int, int);

typedef struct Ctx {
  void *pCtx;
  xTokenFn xToken;
  int base;                /* byte offset of the segment passed to the parent */
} Ctx;

/* Han, Hiragana, Katakana, Hangul. CJK punctuation (U+3000-303F) is not included
** so unicode61 treats it as a separator. */
static int isCJK(unsigned int c){
  return (c >= 0x3400 && c <= 0x4DBF) || (c >= 0x4E00 && c <= 0x9FFF)
      || (c >= 0xF900 && c <= 0xFAFF) || (c >= 0x20000 && c <= 0x3FFFD)
      || (c >= 0x3040 && c <= 0x30FF) || (c >= 0x31F0 && c <= 0x31FF)
      || (c >= 0xFF66 && c <= 0xFF9F)
      || (c >= 0x1100 && c <= 0x11FF) || (c >= 0x3130 && c <= 0x318F)
      || (c >= 0xAC00 && c <= 0xD7AF);
}

/* Decode one UTF-8 code point; returns byte length (1 for invalid bytes). */
static int utf8Decode(const unsigned char *z, int n, unsigned int *pc){
  unsigned char b = z[0];
  int len = b < 0x80 ? 1 : (b & 0xE0) == 0xC0 ? 2 : (b & 0xF0) == 0xE0 ? 3 : (b & 0xF8) == 0xF0 ? 4 : 1;
  if( len > n ) len = 1;
  unsigned int c = len == 1 ? b : len == 2 ? (b & 0x1F) : len == 3 ? (b & 0x0F) : (b & 0x07);
  for(int i = 1; i < len; i++){
    if( (z[i] & 0xC0) != 0x80 ){ *pc = b; return 1; }
    c = (c << 6) | (z[i] & 0x3F);
  }
  *pc = c;
  return len;
}

static int parentCb(void *p, int tflags, const char *pTok, int nTok, int iStart, int iEnd){
  Ctx *ctx = (Ctx*)p;
  int i;
  for(i = 0; i + 2 < nTok; i++){
    if( (unsigned char)pTok[i] == 0xEF && ((unsigned char)pTok[i+1] == 0xBC || (unsigned char)pTok[i+1] == 0xBD)
        && (unsigned char)pTok[i+2] >= 0x80 ) break;
  }
  if( i + 2 >= nTok )
    return ctx->xToken(ctx->pCtx, tflags, pTok, nTok, ctx->base + iStart, ctx->base + iEnd);
  char *z = sqlite3_malloc(nTok);
  if( !z ) return SQLITE_NOMEM;
  int n = 0;
  for(i = 0; i < nTok; ){
    unsigned int cp;
    int len = utf8Decode((const unsigned char*)pTok + i, nTok - i, &cp);
    if( cp >= 0xFF01 && cp <= 0xFF5E ){
      z[n++] = cp - 0xFEE0;
    }else{
      memcpy(z + n, pTok + i, len);
      n += len;
    }
    i += len;
  }
  int rc = ctx->xToken(ctx->pCtx, tflags, z, n, ctx->base + iStart, ctx->base + iEnd);
  sqlite3_free(z);
  return rc;
}

static int cjkCreate(void *pUser, const char **azArg, int nArg, Fts5Tokenizer **ppOut){
  fts5_api *pApi = (fts5_api*)pUser;
  CjkTokenizer *p = sqlite3_malloc(sizeof(*p));
  if( !p ) return SQLITE_NOMEM;
  memset(p, 0, sizeof(*p));
  const char **parentArgs = sqlite3_malloc64(((sqlite3_uint64)nArg + 1) * sizeof(*parentArgs));
  if( !parentArgs ){ sqlite3_free(p); return SQLITE_NOMEM; }
  int nParent = 0, seen = 0, rc = SQLITE_OK;
  for(int i = 0; i < nArg && rc == SQLITE_OK; i++){
    if( strcmp(azArg[i], "unigram") == 0 ){
      if( seen || ++i == nArg || (strcmp(azArg[i], "0") && strcmp(azArg[i], "1")) ){
        rc = SQLITE_ERROR;
      }else{
        p->unigram = azArg[i][0] == '1';
        seen = 1;
      }
    }else{
      parentArgs[nParent++] = azArg[i];
    }
  }
  void *pParentUser = 0;
  if( rc == SQLITE_OK ) rc = pApi->xFindTokenizer(pApi, "unicode61", &pParentUser, &p->parent);
  if( rc == SQLITE_OK ) rc = p->parent.xCreate(pParentUser, parentArgs, nParent, &p->pParent);
  sqlite3_free(parentArgs);
  if( rc != SQLITE_OK ){ sqlite3_free(p); return rc; }
  *ppOut = (Fts5Tokenizer*)p;
  return SQLITE_OK;
}

static void cjkDelete(Fts5Tokenizer *pTok){
  CjkTokenizer *p = (CjkTokenizer*)pTok;
  if( p->pParent ) p->parent.xDelete(p->pParent);
  sqlite3_free(p);
}

typedef struct Rune {
  unsigned int cp;
  int start, end;           /* offsets in the original input */
} Rune;

typedef struct Variant { unsigned int from, to; } Variant;
static const Variant variants[] = {
  {0x518C, 0x518A}, {0x5553, 0x555F}, {0x5CEF, 0x5CF0},
  {0x654E, 0x6559}, {0x6DF8, 0x6E05}, {0x7232, 0x70BA},
  {0x771E, 0x771F}, {0x7955, 0x79D8}, {0x7DAB, 0x7DDA},
  {0x7FA3, 0x7FA4}, {0x81FA, 0x53F0}, {0x885E, 0x885B},
  {0x88E1, 0x88CF}, {0x9751, 0x9752}
};

static int variantCmp(const void *key, const void *entry){
  unsigned int a = *(const unsigned int*)key, b = ((const Variant*)entry)->from;
  return (a > b) - (a < b);
}

static unsigned int fold(unsigned int cp){
  /* U+FF66..FF9F, including the two standalone voiced marks. */
  static const unsigned short kana[] = {
    0x30F2, 0x30A1, 0x30A3, 0x30A5, 0x30A7, 0x30A9, 0x30E3, 0x30E5,
    0x30E7, 0x30C3, 0x30FC, 0x30A2, 0x30A4, 0x30A6, 0x30A8, 0x30AA,
    0x30AB, 0x30AD, 0x30AF, 0x30B1, 0x30B3, 0x30B5, 0x30B7, 0x30B9,
    0x30BB, 0x30BD, 0x30BF, 0x30C1, 0x30C4, 0x30C6, 0x30C8, 0x30CA,
    0x30CB, 0x30CC, 0x30CD, 0x30CE, 0x30CF, 0x30D2, 0x30D5, 0x30D8,
    0x30DB, 0x30DE, 0x30DF, 0x30E0, 0x30E1, 0x30E2, 0x30E4, 0x30E6,
    0x30E8, 0x30E9, 0x30EA, 0x30EB, 0x30EC, 0x30ED, 0x30EF, 0x30F3,
    0x309B, 0x309C
  };
  if( cp >= 0xFF66 && cp <= 0xFF9F ) return kana[cp - 0xFF66];
  const Variant *v = bsearch(&cp, variants, sizeof(variants)/sizeof(*variants), sizeof(*variants), variantCmp);
  return v ? v->to : cp;
}

static unsigned int voiced(unsigned int cp, unsigned int mark){
  if( mark == 0xFF9F ) return cp >= 0x30CF && cp <= 0x30DB && (cp - 0x30CF)%3 == 0 ? cp + 2 : 0;
  if( cp == 0x30A6 ) return 0x30F4;  /* ウ → ヴ */
  if( (cp >= 0x30AB && cp <= 0x30C1 && (cp - 0x30AB)%2 == 0)
      || cp == 0x30C4 || cp == 0x30C6 || cp == 0x30C8 ) return cp + 1;
  if( cp >= 0x30CF && cp <= 0x30DB && (cp - 0x30CF)%3 == 0 ) return cp + 1;
  if( cp == 0x30EF ) return 0x30F7;
  if( cp == 0x30F2 ) return 0x30FA;
  return 0;
}

static int utf8Encode(unsigned int cp, char *z){
  if( cp < 0x80 ){ z[0] = cp; return 1; }
  if( cp < 0x800 ){ z[0] = 0xC0 | (cp >> 6); z[1] = 0x80 | (cp & 63); return 2; }
  if( cp < 0x10000 ){
    z[0] = 0xE0 | (cp >> 12); z[1] = 0x80 | ((cp >> 6) & 63); z[2] = 0x80 | (cp & 63); return 3;
  }
  z[0] = 0xF0 | (cp >> 18); z[1] = 0x80 | ((cp >> 12) & 63);
  z[2] = 0x80 | ((cp >> 6) & 63); z[3] = 0x80 | (cp & 63);
  return 4;
}

/* Emit folded bigrams, keeping offsets in the original CJK run. */
static int emitBigrams(Ctx *ctx, const char *text, int iStart, int iEnd, int unigram, int query){
  Rune *a = sqlite3_malloc64((sqlite3_uint64)(iEnd - iStart) * sizeof(*a));
  if( !a ) return SQLITE_NOMEM;
  int n = 0, rc = SQLITE_OK;
  for(int i = iStart; i < iEnd; ){
    unsigned int cp;
    int len = utf8Decode((const unsigned char*)text + i, iEnd - i, &cp);
    if( (cp == 0xFF9E || cp == 0xFF9F) && n ){
      unsigned int combined = voiced(a[n-1].cp, cp);
      if( combined ){
        a[n-1].cp = combined;
        a[n-1].end = i + len;
        i += len;
        continue;
      }
    }
    a[n++] = (Rune){ fold(cp), i, i + len };
    i += len;
  }
  for(int j = 0; j < n && rc == SQLITE_OK; j++){
    char z[8];
    int size = utf8Encode(a[j].cp, z);
    if( j + 1 < n ){
      if( unigram && !query ){
        rc = ctx->xToken(ctx->pCtx, 0, z, size, a[j].start, a[j].end);
        if( rc != SQLITE_OK ) break;
        size += utf8Encode(a[j+1].cp, z + size);
      }else{
        size += utf8Encode(a[j+1].cp, z + size);
      }
      rc = ctx->xToken(ctx->pCtx, unigram && !query ? FTS5_TOKEN_COLOCATED : 0, z, size, a[j].start, a[j+1].end);
    }else if( n == 1 || unigram ){
      /* A trailing query unigram spans the final character for highlight(). */
      rc = ctx->xToken(ctx->pCtx, 0, z, size, a[j].start, a[j].end);
    }
  }
  sqlite3_free(a);
  return rc;
}

static int cjkTokenize(Fts5Tokenizer *pTok, void *pCtx, int flags, const char *pText, int nText, xTokenFn xToken){
  CjkTokenizer *p = (CjkTokenizer*)pTok;
  Ctx ctx = { pCtx, xToken, 0 };
  int i = 0, segStart = 0, rc = SQLITE_OK;
  int inCJK = 0;
  unsigned int c;
  while( i <= nText && rc == SQLITE_OK ){
    int len = 0, cjk = 0;
    if( i < nText ){
      len = utf8Decode((const unsigned char*)pText + i, nText - i, &c);
      cjk = isCJK(c);
    }
    if( i == nText || cjk != inCJK ){
      if( i > segStart ){
        if( inCJK ){
          rc = emitBigrams(&ctx, pText, segStart, i, p->unigram, flags & FTS5_TOKENIZE_QUERY);
        }else{
          ctx.base = segStart;
          rc = p->parent.xTokenize(p->pParent, &ctx, flags, pText + segStart, i - segStart, parentCb);
        }
      }
      segStart = i;
      inCJK = cjk;
    }
    if( i == nText ) break;
    i += len;
  }
  return rc;
}

static fts5_api *fts5ApiFromDb(sqlite3 *db){
  fts5_api *pApi = 0;
  sqlite3_stmt *pStmt = 0;
  if( sqlite3_prepare_v2(db, "SELECT fts5(?1)", -1, &pStmt, 0) == SQLITE_OK ){
    sqlite3_bind_pointer(pStmt, 1, (void*)&pApi, "fts5_api_ptr", 0);
    sqlite3_step(pStmt);
  }
  sqlite3_finalize(pStmt);
  return pApi;
}

#ifdef _WIN32
__declspec(dllexport)
#endif
int sqlite3_cjk_init(sqlite3 *db, char **pzErr, const sqlite3_api_routines *pApiRoutines){
  SQLITE_EXTENSION_INIT2(pApiRoutines);
  /* The driver's 5s lock policy is installed after auto-extensions. Apply it
     before preparing SQL, or a concurrent writer looks like missing FTS5. */
  sqlite3_busy_timeout(db, 5000);
  fts5_api *pApi = fts5ApiFromDb(db);
  if( !pApi ){
    *pzErr = sqlite3_mprintf("cjk: fts5 not available");
    return SQLITE_ERROR;
  }
  static fts5_tokenizer tok = { cjkCreate, cjkDelete, cjkTokenize };
  return pApi->xCreateTokenizer(pApi, "cjk", (void*)pApi, &tok, 0);
}
