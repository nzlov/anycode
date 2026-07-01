import{i as e,o as t,s as n}from"./workbench-B6dT6y1n.js";var r=`anycode.accessKey`,i=`/graphql`;function a(){return typeof window>`u`?``:window.localStorage.getItem(`anycode.accessKey`)??window.localStorage.getItem(`ANYCODE_ACCESS_KEY`)??``}function o(e){if(!(typeof window>`u`)){if(e.trim()===``){window.localStorage.removeItem(r);return}window.localStorage.setItem(r,e.trim())}}async function s({query:e,variables:t,operationName:n}){let r=new Headers({"content-type":`application/json`}),o=a();o&&r.set(`authorization`,`Bearer ${o}`);let s=await fetch(i,{method:`POST`,headers:r,body:JSON.stringify({query:e,variables:t,operationName:n})});if(!s.ok)throw Error(`GraphQL request failed: ${s.status}`);let c=await s.json();if(c.errors?.length)throw Error(c.errors.map(e=>e.message).join(`; `));if(!c.data)throw Error(`GraphQL response missing data`);return c.data}var c=`
  id
  projectId
  projectName
  requirement
  requirementSummary
  mode
  status
  baseBranch
  currentNodeTitle
  pendingQuestion
  lastRunAt
  createdAt
  updatedAt
`,l=`
  id
  projectId
  requirement
  mode
  status
  baseBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  lastRunAt
  createdAt
  updatedAt
`;async function u(e={}){try{let t=await s({query:`
        query Sessions($input: ListSessionsInput) {
          sessions(input: $input) {
            items {
              ${c}
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      `,variables:{input:e}});return{items:t.sessions.items.map(m),pageInfo:t.sessions.pageInfo}}catch{return _(e)}}async function d(n){try{let e=await s({query:`
          query Session($id: ID!) {
            session(id: $id) {
              ${l}
            }
          }
        `,variables:{id:n}}),r=[...t];try{r=(await s({query:`
          query SessionEvents($input: ListSessionEventsInput!) {
            sessionEvents(input: $input) {
              items {
                id
                type
                payload
                createdAt
              }
              pageInfo {
                page
                pageSize
                total
                nextCursor
              }
            }
          }
        `,variables:{input:{sessionId:n,page:1,pageSize:50}}})).sessionEvents.items.map(g)}catch{r=[...t]}return{session:h(e.session),events:r}}catch{return{session:e(n),events:[...t]}}}async function f(e,t){try{return await s({query:`
        mutation AppendPrompt($input: AppendPromptInput!) {
          appendPrompt(input: $input) {
            id
            sessionId
            body
            createdAt
          }
        }
      `,variables:{input:{sessionId:e,body:t}}})}catch{return{appendPrompt:{id:`local-${Date.now()}`,sessionId:e,body:t,createdAt:new Date().toISOString()}}}}async function p(e){try{return h((await s({query:`
        mutation CreateSession($input: CreateSessionInput!) {
          createSession(input: $input) {
            ${l}
          }
        }
      `,variables:{input:e}})).createSession)}catch{return h({id:`local-${Date.now()}`,projectId:e.projectId,requirement:e.requirement,mode:e.mode,status:`stopped`,baseBranch:e.baseBranch??`main`,config:{codexModel:e.config?.codexModel??``,reasoningEffort:e.config?.reasoningEffort??``,permissionMode:e.config?.permissionMode??``},lastRunAt:null,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()})}}function m(e){return{id:e.id,projectId:e.projectId,title:e.requirementSummary||w(e.requirement),summary:e.requirementSummary||e.requirement,mode:v(e.mode),status:y(e.status),branch:e.baseBranch||`main`,node:e.currentNodeTitle||S(y(e.status)),updatedAt:T(e.lastRunAt??e.updatedAt),pendingQuestion:e.pendingQuestion,filesChanged:0}}function h(e){let t=y(e.status);return{id:e.id,projectId:e.projectId,title:w(e.requirement),summary:e.requirement,mode:v(e.mode),status:t,branch:e.baseBranch||`main`,node:S(t),updatedAt:T(e.lastRunAt??e.updatedAt),pendingQuestion:t===`waiting_user`,filesChanged:0}}function g(e){return{id:e.id,kind:b(e.type),title:C(e.payload,`title`)||x(e.type),body:C(e.payload,`body`)||C(e.payload,`message`)||JSON.stringify(e.payload),time:E(e.createdAt)}}function _(e){let t=e.page??1,r=e.pageSize??n.length,i=(t-1)*r;return{items:n.slice(i,i+r),pageInfo:{page:t,pageSize:r,total:n.length,nextCursor:i+r<n.length?String(t+1):``}}}function v(e){return e===`chat`?`chat`:`workflow`}function y(e){return e===`running`||e===`waiting_user`||e===`stopped`||e===`blocked`||e===`completed`?e:`stopped`}function b(e){return e.includes(`tool`)?`tool`:e.includes(`assistant`)?`assistant`:e.includes(`question`)?`question`:e.includes(`thought`)?`thought`:`status`}function x(e){return e.includes(`tool`)?`工具调用`:e.includes(`assistant`)?`模型输出`:e.includes(`question`)?`待回答`:e.includes(`thought`)?`思考`:`状态`}function S(e){return{running:`运行中`,waiting_user:`待回答`,stopped:`已停止`,blocked:`阻塞`,completed:`已完成`}[e]}function C(e,t){let n=e[t];return typeof n==`string`?n:``}function w(e){return e.split(`
`).find(e=>e.trim())?.trim()||`未命名会话`}function T(e){return e?new Intl.DateTimeFormat(`zh-CN`,{month:`2-digit`,day:`2-digit`,hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}function E(e){return e?new Intl.DateTimeFormat(`zh-CN`,{hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}export{a,u as i,p as n,s as o,d as r,o as s,f as t};