#!/usr/bin/env python

# Copyright (c) 2012 Google Inc. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

"""Utility functions for Windows builds.

These functions are executed via gyp-win-tool when using the ninja generator.
"""

import os
import re
import shutil
import subprocess
import sys

BASE_DIR = os.path.dirname(os.path.abspath(__file__))

# A regex matching an argument corresponding to a PDB filename passed as an
# argument to link.exe.
_LINK_EXE_PDB_ARG = re.compile('/PDB:(?P<pdb>.+\.exe\.pdb)$', re.IGNORECASE)

def main(args):
  executor = WinTool()
  exit_code = executor.Dispatch(args)
  if exit_code is not None:
    sys.exit(exit_code)


class WinTool(object):
  """This class performs all the Windows tooling steps. The methods can either
  be executed directly, or dispatched from an argument list."""

  def _MaybeUseSeparateMspdbsrv(self, env, args):
    """Allows to use a unique instance of mspdbsrv.exe for the linkers linking
    an .exe target if GYP_USE_SEPARATE_MSPDBSRV has been set."""
    if not os.environ.get('GYP_USE_SEPARATE_MSPDBSRV'):
      return

    if len(args) < 1:
      raise Exception("Not enough arguments")

    if args[0] != 'link.exe':
      return

    # Checks if this linker produces a PDB for an .exe target. If so use the
    # name of this PDB to generate an endpoint name for mspdbsrv.exe.
    endpoint_name = None
    for arg in args:
      m = _LINK_EXE_PDB_ARG.match(arg)
      if m:
        endpoint_name = '%s_%d' % (m.group('pdb'), os.getpid())
        break

    if endpoint_name is None:
      return

    # Adds the appropriate environment variable. This will be read by link.exe
    # to know which instance of mspdbsrv.exe it should connect to (if it's
    # not set then the default endpoint is used).
    env['_MSPDBSRV_ENDPOINT_'] = endpoint_name

  def Dispatch(self, args):
    """Dispatches a string command to a method."""
    if len(args) < 1:
      raise Exception("Not enough arguments")

    method = "Exec%s" % self._CommandifyName(args[0])
    return getattr(self, method)(*args[1:])

  def _CommandifyName(self, name_string):
    """Transforms a tool name like recursive-mirror to RecursiveMirror."""
    return name_string.title().replace('-', '')

  def _GetEnv(self, arch):
    """Gets the saved environment from a file for a given architecture."""
    # The environment is saved as an "environment block" (see CreateProcess
    # and msvs_emulation for details). We convert to a dict here.
    # Drop last 2 NULs, one for list terminator, one for trailing vs. separator.
    pairs = open(arch).read()[:-2].split('\0')
    kvs = [item.split('=', 1) for item in pairs]
    return dict(kvs)

  def ExecStamp(self, path):
    """Simple stamp command."""
    open(path, 'w').close()

  def ExecRecursiveMirror(self, source, dest):
    """Emulation of rm -rf out && cp -af in out."""
    if os.path.exists(dest):
      if os.path.isdir(dest):
        shutil.rmtree(dest)
      else:
        os.unlink(dest)
    if os.path.isdir(source):
      shutil.copytree(source, dest)
    else:
      shutil.copy2(source, dest)

  def ExecLinkWrapper(self, arch, *args):
    """Filter diagnostic output from link that looks like:
    '   Creating library ui.dll.lib and object ui.dll.exp'
    This happens when there are exports from the dll or exe.
    """
    env = self._GetEnv(arch)
    self._MaybeUseSeparateMspdbsrv(env, args)
    link = subprocess.Popen(args,
                            shell=True,
                            env=env,
                            stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT)
    out, _ = link.communicate()
    for line in out.splitlines():
      if not line.startswith('   Creating library '):
        print line
    return link.returncode

  def ExecManifestWrapper(self, arch, *args):
    """Run manifest tool with environment set. Strip out undesirable warning
    (some XML blocks are recognized by the OS loader, but not the manifest
    tool)."""
    env = self._GetEnv(arch)
    popen = subprocess.Popen(args, shell=True, env=env,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    out, _ = popen.communicate()
    for line in out.splitlines():
      if line and 'manifest authoring warning 81010002' not in line:
        print line
    return popen.returncode

  def ExecManifestToRc(self, arch, *args):
    """Creates a resource file pointing a SxS assembly manifest.
    |args| is tuple containing path to resource file, path to manifest file
    and resource name which can be "1" (for executables) or "2" (for DLLs)."""
    manifest_path, resource_path, resource_name = args
    with open(resource_path, 'wb') as output:
      output.write('#include <windows.h>\n%s RT_MANIFEST "%s"' % (
        resource_name,
        os.path.abspath(manifest_path).replace('\\', '/')))

  def ExecMidlWrapper(self, arch, outdir, tlb, h, dlldata, iid, proxy, idl,
                      *flags):
    """Filter noisy filenames output from MIDL compile step that isn't
    quietable via command line flags.
    """
    args = ['midl', '/nologo'] + list(flags) + [
        '/out', outdir,
        '/tlb', tlb,
        '/h', h,
        '/dlldata', dlldata,
        '/iid', iid,
        '/proxy', proxy,
        idl]
    env = self._GetEnv(arch)
    popen = subprocess.Popen(args, shell=True, env=env,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    out, _ = popen.communicate()
    # Filter junk out of stdout, and write filtered versions. Output we want
    # to filter is pairs of lines that look like this:
    # Processing C:\Program Files (x86)\Microsoft SDKs\...\include\objidl.idl
    # objidl.idl
    lines = out.splitlines()
    prefix = 'Processing '
    processing = set(os.path.basename(x) for x in lines if x.startswith(prefix))
    for line in lines:
      if not line.startswith(prefix) and line not in processing:
        print line
    return popen.returncode

  def ExecAsmWrapper(self, arch, *args):
    """Filter logo banner from invocations of asm.exe."""
    env = self._GetEnv(arch)
    # MSVS doesn't assemble x64 asm files.
    if arch == 'environment.x64':
      return 0
    popen = subprocess.Popen(args, shell=True, env=env,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    out, _ = popen.communicate()
    for line in out.splitlines():
      if (not line.startswith('Copyright (C) Microsoft Corporation') and
          not line.startswith('Microsoft (R) Macro Assembler') and
          not line.startswith(' Assembling: ') and
          line):
        print line
    return popen.returncode

  def ExecRcWrapper(self, arch, *args):
    """Filter logo banner from invocations of rc.exe. Older versions of RC
    don't support the /nologo flag."""
    env = self._GetEnv(arch)
    popen = subprocess.Popen(args, shell=True, env=env,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    out, _ = popen.communicate()
    for line in out.splitlines():
      if (not line.startswith('Microsoft (R) Windows (R) Resource Compiler') and
          not line.startswith('Copyright (C) Microsoft Corporation') and
          line):
        print line
    return popen.returncode

  def ExecActionWrapper(self, arch, rspfile, *dir):
    """Runs an action command line from a response file using the environment
    for |arch|. If |dir| is supplied, use that as the working directory."""
    env = self._GetEnv(arch)
    args = open(rspfile).read()
    dir = dir[0] if dir else None
    return subprocess.call(args, shell=True, env=env, cwd=dir)

if __name__ == '__main__':
  sys.exit(main(sys.argv[1:]))
